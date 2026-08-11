// Package rpc is a client for the Nimiq Albatross JSON-RPC interface.
//
// It holds no key material and cannot sign. Transactions are built and signed
// with the nimiq and nimiq/signer packages; this package only moves already
// serialized bytes to a node and reads chain state back. A service that only
// reads the chain can import this package and nimiq, and never link
// key-handling code at all.
//
// The endpoint is always supplied by the caller. No public endpoint is baked in.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Client talks to one Nimiq JSON-RPC endpoint.
//
// A Client is safe for concurrent use.
type Client struct {
	// urls holds the primary endpoint followed by any fallbacks. Read methods
	// walk the list on transport failure; see call.
	urls       []string
	httpClient *http.Client
	id         atomic.Int64
	user, pass string
	// network is the chain transactions built by Send are signed for. Set with
	// WithNetwork; it is never read from the node.
	network nimiq.NetworkID
	hasNetwork bool
	// onRequest, when set, is called after every attempt. It is the hook for
	// metrics and logging, so the SDK itself needs no logger dependency.
	onRequest func(Observation)
}

// Observation reports one completed RPC attempt to the hook installed with
// WithObserver.
type Observation struct {
	Method   string
	Duration time.Duration
	Err      error
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient supplies the HTTP client, and with it the transport, proxy and
// overall timeout policy.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithBasicAuth sets HTTP basic auth credentials, as a private node or a
// metered RPC provider may require.
func WithBasicAuth(user, pass string) Option {
	return func(c *Client) { c.user, c.pass = user, pass }
}

// WithObserver installs a callback invoked after every request. Use it for
// metrics or logging; it must not block.
func WithObserver(f func(Observation)) Option {
	return func(c *Client) { c.onRequest = f }
}

// WithFallbackEndpoints adds endpoints to try when an earlier one is
// unreachable, so an application can point at an RPC cluster without any
// application code changing.
//
// Failover happens only on transport-level failures: a connection error, a
// timeout, or an HTTP 5xx. A JSON-RPC error is a considered answer from a
// working node — "insufficient funds" means the same thing everywhere — so
// retrying it elsewhere would only turn one clear error into several slow ones.
//
// This is safe for broadcasting as well as reading. A transaction hash covers
// its signed content, so submitting identical bytes to a second endpoint after
// the first connection dropped is a no-op if the first attempt actually landed.
// Failover must therefore never rebuild a transaction, only resend it.
func WithFallbackEndpoints(urls ...string) Option {
	return func(c *Client) {
		for _, u := range urls {
			if u != "" {
				c.urls = append(c.urls, u)
			}
		}
	}
}

// WithNetwork sets the network transactions are signed for. Required for
// Send; it is not read from the node, since a node's answer is not
// authority over what your application meant to sign.
func WithNetwork(n nimiq.NetworkID) Option {
	return func(c *Client) {
		c.network = n
		c.hasNetwork = true
	}
}

// Endpoints returns the endpoints this client will try, in order.
func (c *Client) Endpoints() []string {
	return append([]string(nil), c.urls...)
}

// New creates a client for the given endpoint URL.
//
// The URL is required and never defaulted: which chain a service talks to is an
// operational decision, and silently falling back to a public endpoint would
// hide a misconfiguration until it reached production.
func New(endpoint string, opts ...Option) (*Client, error) {
	if endpoint == "" {
		return nil, errors.New("nimiq/rpc: endpoint URL is required")
	}
	c := &Client{
		urls:       []string{endpoint},
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Error is a JSON-RPC error returned by the node.
type Error struct {
	Method  string
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *Error) Error() string {
	s := fmt.Sprintf("nimiq/rpc: %s failed: %s (code %d)", e.Method, e.Message, e.Code)
	if len(e.Data) > 0 && string(e.Data) != "null" {
		s += " data=" + string(e.Data)
	}
	return s
}

// HTTPError reports a non-2xx response, which is distinct from a JSON-RPC
// error: it usually means auth, routing or rate limiting rather than anything
// about the request's content.
type HTTPError struct {
	Method string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("nimiq/rpc: %s got HTTP %d: %s", e.Method, e.Status, e.Body)
}

// Call issues a raw JSON-RPC request for methods this SDK does not wrap,
// with the client's retries, timeouts, and endpoint failover applied.
// ponytail: escape hatch instead of wrapping every albatross method.
func (c *Client) Call(ctx context.Context, method string, params []any, out any) error {
	return c.call(ctx, method, params, out)
}

// call performs a JSON-RPC request, walking the configured endpoints until one
// answers.
//
// Only transport-level failures move to the next endpoint. A JSON-RPC error is
// a considered answer from a working node and is returned immediately: retrying
// "insufficient funds" against three more endpoints produces three more of the
// same error, more slowly. Within a single endpoint there is no retry at all,
// because whether repeating a method is safe depends on the method, and only
// the caller knows.
func (c *Client) call(ctx context.Context, method string, params []any, out any) (err error) {
	if params == nil {
		params = []any{}
	}
	started := time.Now()
	defer func() {
		if c.onRequest != nil {
			c.onRequest(Observation{Method: method, Duration: time.Since(started), Err: err})
		}
	}()

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": c.id.Add(1), "method": method, "params": params,
	})
	if err != nil {
		return fmt.Errorf("nimiq/rpc: encoding %s request: %w", method, err)
	}

	var lastErr error
	for i, url := range c.urls {
		err := c.callOne(ctx, url, method, body, out)
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		// The context expiring is not an endpoint's fault, and trying another
		// would only burn a deadline the caller already set.
		if ctx.Err() != nil {
			return err
		}
		lastErr = fmt.Errorf("nimiq/rpc: endpoint %d/%d: %w", i+1, len(c.urls), err)
	}
	return lastErr
}

// isRetryable reports whether an error justifies trying another endpoint: a
// transport failure, or a 5xx that says the node is unwell rather than that the
// request was wrong.
func isRetryable(err error) bool {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status >= 500
	}
	var rpcErr *Error
	return !errors.As(err, &rpcErr)
}

func (c *Client) callOne(ctx context.Context, url, method string, body []byte, out any) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("nimiq/rpc: building %s request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nimiq/rpc: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Bound the response so a malfunctioning endpoint cannot exhaust memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return fmt.Errorf("nimiq/rpc: reading %s response: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Method: method, Status: resp.StatusCode, Body: truncate(string(raw), 512)}
	}

	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("nimiq/rpc: decoding %s response: %w (body: %s)", method, err, truncate(string(raw), 512))
	}
	if envelope.Error != nil {
		return &Error{Method: method, Code: envelope.Error.Code, Message: envelope.Error.Message, Data: envelope.Error.Data}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(unwrap(envelope.Result), out)
}

// unwrap strips the {"data": …, "metadata": …} envelope Albatross RPC wraps
// results in.
//
// A transaction object also has a "data" field, so the envelope is only
// recognised when "metadata" is present alongside it.
func unwrap(result json.RawMessage) json.RawMessage {
	var probe map[string]json.RawMessage
	if json.Unmarshal(result, &probe) != nil {
		return result
	}
	data, hasData := probe["data"]
	if _, hasMetadata := probe["metadata"]; hasData && hasMetadata {
		return data
	}
	return result
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// BlockNumber returns the current chain head height.
//
// Use it to obtain a validity start height for a transaction being built. That
// is the only network call transaction construction needs, and it is separate
// from signing by design.
func (c *Client) BlockNumber(ctx context.Context) (uint32, error) {
	var height uint32
	err := c.call(ctx, "getBlockNumber", nil, &height)
	return height, err
}

// MacroBlockAtOrBefore returns the height of the last macro block at or before
// height.
//
// This is a policy calculation, not a chain lookup: getLastMacroBlock takes a
// height and returns a bare integer. Calling it with no arguments — or decoding
// its result as a block object — fails.
func (c *Client) MacroBlockAtOrBefore(ctx context.Context, height uint32) (uint32, error) {
	var macro uint32
	err := c.call(ctx, "getLastMacroBlock", []any{height}, &macro)
	return macro, err
}

// LastMacroBlockNumber returns the height of the most recent macro block.
//
// Under Albatross this is the meaningful notion of finality: a transaction at or
// below this height is settled, whereas inclusion in a micro block can still be
// reverted. Counting confirmations the way Ethereum does is the wrong model.
//
// This costs two round trips, since the macro height is derived from the head.
// Callers polling in a loop should call BlockNumber and MacroBlockAtOrBefore
// directly and reuse the head they already have.
func (c *Client) LastMacroBlockNumber(ctx context.Context) (uint32, error) {
	head, err := c.BlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	return c.MacroBlockAtOrBefore(ctx, head)
}

// Account is an account's on-chain state.
type Account struct {
	Address string     `json:"address"`
	Balance nimiq.Luna `json:"balance"`
	Type    string     `json:"type"`
}

// GetAccount looks up an account. A never-used address is reported by nodes as
// a basic account with a zero balance rather than as an error.
func (c *Client) GetAccount(ctx context.Context, addr nimiq.Address) (*Account, error) {
	var acc Account
	if err := c.call(ctx, "getAccountByAddress", []any{addr.String()}, &acc); err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetBalance is a convenience wrapper over GetAccount.
func (c *Client) GetBalance(ctx context.Context, addr nimiq.Address) (nimiq.Luna, error) {
	acc, err := c.GetAccount(ctx, addr)
	if err != nil {
		return 0, err
	}
	return acc.Balance, nil
}

// Transaction is a transaction as reported by the chain.
type Transaction struct {
	Hash                string     `json:"hash"`
	BlockNumber         uint32     `json:"blockNumber"`
	Timestamp           int64      `json:"timestamp"`
	From                string     `json:"from"`
	To                  string     `json:"to"`
	Value               nimiq.Luna `json:"value"`
	Fee                 nimiq.Luna `json:"fee"`
	RecipientData       string     `json:"recipientData"`
	SenderData          string     `json:"senderData"`
	Flags               uint8      `json:"flags"`
	ValidityStartHeight uint32     `json:"validityStartHeight"`
	NetworkID           uint8      `json:"networkId"`
	// ExecutionResult reports whether the transaction actually succeeded.
	//
	// Albatross includes failed transactions in blocks: the fee is charged and
	// the transaction is permanently recorded, but its effect is not applied.
	// A staking transaction naming a validator that does not exist is included
	// with ExecutionResult false. Inclusion is therefore not success, and a
	// service that treats "mined" as "done" will report payouts that never
	// happened.
	//
	// Pointer so that a node which omits the field is distinguishable from one
	// reporting an explicit failure.
	ExecutionResult *bool `json:"executionResult"`
}

// Succeeded reports whether the transaction executed successfully. A node that
// does not report the field at all is treated as success, since older
// endpoints omit it and only failures are worth blocking on.
func (t *Transaction) Succeeded() bool {
	return t.ExecutionResult == nil || *t.ExecutionResult
}

// ErrNotFound reports that the chain has no record of what was requested. For a
// transaction this means it is not yet mined, or never was.
var ErrNotFound = errors.New("nimiq/rpc: not found")

// GetTransaction looks up a transaction by hash, returning ErrNotFound when the
// chain has no record of it.
//
// Nodes signal an unknown transaction with an error rather than a null result,
// so it is normalized here: callers polling for inclusion should treat
// ErrNotFound as "not yet", not as a failure.
func (c *Client) GetTransaction(ctx context.Context, hash string) (*Transaction, error) {
	var tx Transaction
	if err := c.call(ctx, "getTransactionByHash", []any{hash}, &tx); err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			return nil, fmt.Errorf("%w: transaction %s", ErrNotFound, hash)
		}
		return nil, err
	}
	if tx.Hash == "" {
		return nil, fmt.Errorf("%w: transaction %s", ErrNotFound, hash)
	}
	return &tx, nil
}

// SendRawTransaction broadcasts an already signed, serialized transaction and
// returns the hash the node computed for it.
//
// This method is NOT retried internally, and callers must be careful about
// retrying it themselves. A network failure leaves it unknown whether the node
// accepted the transaction.
//
// Rebroadcasting the exact same bytes is safe: the transaction hash is derived
// from the signed content, so a duplicate submission is a no-op at the mempool.
// What is not safe is rebuilding the transaction — a fresh validity start height
// produces a different hash and therefore a second, genuinely distinct payment.
// Retry by resending the same serialized bytes, never by rebuilding.
func (c *Client) SendRawTransaction(ctx context.Context, raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", errors.New("nimiq/rpc: refusing to broadcast empty transaction bytes")
	}
	var hash string
	if err := c.call(ctx, "sendRawTransaction", []any{hexEncode(raw)}, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

// SendTransaction serializes and broadcasts a signed transaction, checking that
// the node's hash matches the locally computed one.
//
// A mismatch means the node interpreted the bytes differently than this SDK
// did, so it is reported rather than ignored.
func (c *Client) SendTransaction(ctx context.Context, tx *nimiq.Transaction) (string, error) {
	raw, err := tx.Serialize()
	if err != nil {
		return "", err
	}
	local := tx.Hash().String()
	remote, err := c.SendRawTransaction(ctx, raw)
	if err != nil {
		return "", err
	}
	if remote != "" && remote != local {
		return remote, fmt.Errorf("nimiq/rpc: node reported hash %s but the transaction hashes locally to %s", remote, local)
	}
	return local, nil
}

// Send builds, signs, and broadcasts a basic transfer, filling in the
// validity start height from the chain head and the fee from the network
// minimum. Returns the transaction hash.
func (c *Client) Send(ctx context.Context, s nimiq.Signer, to nimiq.Address, value nimiq.Luna) (string, error) {
	if !c.hasNetwork {
		return "", errors.New("nimiq/rpc: Send requires WithNetwork")
	}
	height, err := c.BlockNumber(ctx)
	if err != nil {
		return "", err
	}
	pub, err := s.PublicKey(ctx)
	if err != nil {
		return "", err
	}
	from, err := nimiq.AddressFromPublicKey(pub)
	if err != nil {
		return "", err
	}
	tx, err := nimiq.NewBasicTransaction(from, to, value, 0, height, c.network)
	if err != nil {
		return "", err
	}
	fee, err := c.EstimateFee(ctx, tx)
	if err != nil {
		return "", err
	}
	tx.Fee = fee
	if err := nimiq.SignTransaction(ctx, s, tx); err != nil {
		return "", err
	}
	return c.SendTransaction(ctx, tx)
}

func hexEncode(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
