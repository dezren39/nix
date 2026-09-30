package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPTransport speaks MCP Streamable HTTP. Each Send performs a POST; the
// reply is either a single JSON object or an SSE stream, and every JSON-RPC
// message found is queued for Recv.
//
// This covers remote MCP servers directly, which removes the need for an
// mcp-remote node shim in front of them.
type HTTPTransport struct {
	url     string
	headers map[string]string
	hc      *http.Client

	sessionMu sync.RWMutex
	sessionID string

	incoming chan []byte
	errOnce  sync.Once
	errCh    chan error
	err      error

	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// HTTPOptions configure a remote MCP server.
type HTTPOptions struct {
	URL     string
	Headers map[string]string
	Timeout time.Duration
}

// NewHTTP creates a Streamable HTTP transport.
func NewHTTP(opts HTTPOptions) (*HTTPTransport, error) {
	if opts.URL == "" {
		return nil, errors.New("http transport: empty url")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaults.HTTPRequestTimeout
	}
	return &HTTPTransport{
		url:      opts.URL,
		headers:  opts.Headers,
		hc:       &http.Client{Timeout: timeout},
		incoming: make(chan []byte, 64),
		errCh:    make(chan error, 1),
		closed:   make(chan struct{}),
	}, nil
}

func (t *HTTPTransport) setHeaders(req *http.Request) {
	t.setHeadersFor(req, nil)
}

// setHeadersFor sets headers for one outgoing frame.
//
// The version header follows the frame. In 2026-07-28 every request carries
// its version in _meta, and the header MUST match it or the server answers
// 400 -- so a fixed header was correct only until the client started saying
// which version it spoke, and then wrong on every modern request.
func (t *HTTPTransport) setHeadersFor(req *http.Request, msg []byte) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	version := ProtocolVersion
	if v := frameVersion(msg); v != "" {
		version = v
	}
	req.Header.Set("MCP-Protocol-Version", version)
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	t.sessionMu.RLock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.sessionMu.RUnlock()
}

// Send POSTs a frame and dispatches the reply asynchronously.
func (t *HTTPTransport) Send(ctx context.Context, msg []byte) error {
	select {
	case <-t.closed:
		return errors.New("http transport closed")
	default:
	}

	// The caller's context governs the request.
	//
	// Send is synchronous, so a server that accepts the connection and never
	// answers blocks here -- before Call reaches the select that watches for
	// a timeout. Detaching the request from the context therefore did not
	// make cancellation best-effort, it removed it: pool.callTimeout could
	// not interrupt a hung server at all.
	//
	// Protocol-level cancellation is unaffected. The notifications/cancelled
	// message is sent on its own background context precisely so that it
	// outlives the request it is cancelling.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	t.setHeadersFor(req, msg)

	resp, err := t.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", t.url, err)
	}

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionMu.Lock()
		t.sessionID = sid
		t.sessionMu.Unlock()
	}

	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNoContent {
		resp.Body.Close()
		return nil // notification acknowledged, no body
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.HTTPErrorBodyLimit))
		resp.Body.Close()
		he := &HTTPStatusError{URL: t.url, Status: resp.StatusCode, Body: bytes.TrimSpace(b)}
		he.rpc, he.id = parseRPCError(he.Body)
		// A modern server answers a bad request with 400 and a JSON-RPC
		// error naming the request. That is the reply, not a transport
		// failure, and the caller waiting on that id should receive it as
		// one -- which is how an UnsupportedProtocolVersionError reaches the
		// code that knows to retry.
		if he.rpc != nil && he.id != nil && sameID(he.id, msg) {
			t.push(he.Body)
			return nil
		}
		return he
	}

	ct := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			defer resp.Body.Close()
			t.readSSE(resp.Body)
		}()
	default:
		b, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if len(bytes.TrimSpace(b)) > 0 {
			t.push(b)
		}
	}
	return nil
}

func (t *HTTPTransport) readSSE(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var data strings.Builder
	flush := func() {
		if data.Len() == 0 {
			return
		}
		payload := data.String()
		data.Reset()
		if strings.TrimSpace(payload) != "" {
			t.push([]byte(payload))
		}
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
			// comment / keepalive
		case strings.HasPrefix(line, "data:"):
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(v)
		}
	}
	flush()
}

func (t *HTTPTransport) push(b []byte) {
	// A frame may be a single object or a batch array.
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err == nil {
			for _, m := range batch {
				t.deliver(m)
			}
			return
		}
	}
	t.deliver(trimmed)
}

func (t *HTTPTransport) deliver(b []byte) {
	select {
	case t.incoming <- b:
	case <-t.closed:
	}
}

// Recv returns the next queued frame.
func (t *HTTPTransport) Recv() ([]byte, error) {
	select {
	case b := <-t.incoming:
		return b, nil
	case err := <-t.errCh:
		return nil, err
	case <-t.closed:
		if t.err != nil {
			return nil, t.err
		}
		return nil, io.EOF
	}
}

// Close releases the session.
func (t *HTTPTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closed)
		t.sessionMu.RLock()
		sid := t.sessionID
		t.sessionMu.RUnlock()
		if sid != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, nil)
			if err == nil {
				t.setHeaders(req)
				if resp, err := t.hc.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
	})
	return nil
}

// Info describes the endpoint.
func (t *HTTPTransport) Info() string { return t.url }

// frameVersion reads the per-request protocol version from a frame's _meta,
// or "" for a legacy frame that carries none.
func frameVersion(msg []byte) string {
	if len(msg) == 0 {
		return ""
	}
	var f struct {
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}
	if json.Unmarshal(msg, &f) != nil {
		return ""
	}
	v, _ := f.Params.Meta[MetaProtocolVersion].(string)
	return v
}

// HTTPStatusError is a POST answered with a status outside 2xx.
//
// Structured rather than a string because the status and the body are what
// the era probe decides on: a 4xx with a recognised modern JSON-RPC error is
// a modern server, a 4xx with anything else is a legacy one, and a failure
// to connect is neither.
type HTTPStatusError struct {
	URL    string
	Status int
	Body   []byte
	// rpc is the JSON-RPC error in Body, if Body is one; id is its id.
	rpc *rpcError
	id  json.RawMessage
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("post %s: http %d: %s", e.URL, e.Status, string(e.Body))
}

// RPCCode returns the JSON-RPC error code carried in the body, if any.
func (e *HTTPStatusError) RPCCode() (int, bool) {
	if e.rpc == nil {
		return 0, false
	}
	return e.rpc.Code, true
}

// parseRPCError reads a JSON-RPC error response out of a body.
func parseRPCError(body []byte) (*rpcError, json.RawMessage) {
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   *rpcError       `json:"error"`
	}
	if json.Unmarshal(body, &r) != nil || r.JSONRPC != "2.0" || r.Error == nil {
		return nil, nil
	}
	if len(r.ID) == 0 || string(r.ID) == "null" {
		return r.Error, nil
	}
	return r.Error, r.ID
}

// sameID reports whether a response id matches the id of the frame sent.
func sameID(id json.RawMessage, msg []byte) bool {
	var f struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(msg, &f) != nil || len(f.ID) == 0 {
		return false
	}
	return bytes.Equal(bytes.TrimSpace(f.ID), bytes.TrimSpace(id))
}
