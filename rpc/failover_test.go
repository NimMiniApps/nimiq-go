package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// countingServer records how many requests it received and replies with a
// fixed body and status.
func countingServer(t *testing.T, status int, body string) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != 200 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

// deadEndpoint returns a URL that refuses connections.
func deadEndpoint(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now
	return url
}

func TestFailoverOnDeadEndpoint(t *testing.T) {
	good, hits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"result":42}`)
	c, err := New(deadEndpoint(t), WithFallbackEndpoints(good))
	if err != nil {
		t.Fatal(err)
	}
	height, err := c.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("should have failed over: %v", err)
	}
	if height != 42 {
		t.Errorf("height = %d, want 42", height)
	}
	if hits.Load() != 1 {
		t.Errorf("fallback hit %d times, want 1", hits.Load())
	}
}

func TestFailoverOn5xx(t *testing.T) {
	bad, badHits := countingServer(t, 503, "unavailable")
	good, goodHits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"result":7}`)
	c, err := New(bad, WithFallbackEndpoints(good))
	if err != nil {
		t.Fatal(err)
	}
	height, err := c.BlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if height != 7 || badHits.Load() != 1 || goodHits.Load() != 1 {
		t.Errorf("height=%d badHits=%d goodHits=%d", height, badHits.Load(), goodHits.Load())
	}
}

// TestNoFailoverOnRPCError is the important negative. A JSON-RPC error is a
// considered answer from a working node; retrying it elsewhere turns one clear
// error into several slow ones and hides the real cause.
func TestNoFailoverOnRPCError(t *testing.T) {
	bad, badHits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Invalid params"}}`)
	other, otherHits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"result":1}`)
	c, err := New(bad, WithFallbackEndpoints(other))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockNumber(context.Background()); err == nil {
		t.Fatal("expected the RPC error to surface")
	}
	if badHits.Load() != 1 {
		t.Errorf("first endpoint hit %d times, want 1", badHits.Load())
	}
	if otherHits.Load() != 0 {
		t.Error("an RPC error must not be retried against another endpoint")
	}
}

// TestNoFailoverOn4xx: a 401 or 429 is about this request, not node health.
func TestNoFailoverOnAuthFailure(t *testing.T) {
	bad, _ := countingServer(t, 401, "denied")
	other, otherHits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"result":1}`)
	c, err := New(bad, WithFallbackEndpoints(other))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockNumber(context.Background()); err == nil {
		t.Fatal("expected the auth failure to surface")
	}
	if otherHits.Load() != 0 {
		t.Error("a 4xx must not trigger failover")
	}
}

func TestAllEndpointsDown(t *testing.T) {
	c, err := New(deadEndpoint(t), WithFallbackEndpoints(deadEndpoint(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockNumber(context.Background()); err == nil {
		t.Fatal("expected an error when every endpoint is down")
	}
}

func TestEndpointsOrder(t *testing.T) {
	c, err := New("http://a", WithFallbackEndpoints("http://b", "", "http://c"))
	if err != nil {
		t.Fatal(err)
	}
	got := c.Endpoints()
	want := []string{"http://a", "http://b", "http://c"}
	if len(got) != len(want) {
		t.Fatalf("endpoints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("endpoint %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// TestCancelledContextStopsFailover: burning a caller's deadline against every
// endpoint in turn is worse than failing fast.
func TestCancelledContextStopsFailover(t *testing.T) {
	_, hits := countingServer(t, 200, `{"jsonrpc":"2.0","id":1,"result":1}`)
	c, err := New(deadEndpoint(t), WithFallbackEndpoints(deadEndpoint(t)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.BlockNumber(ctx); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if hits.Load() != 0 {
		t.Error("no request should have been made with a cancelled context")
	}
}
