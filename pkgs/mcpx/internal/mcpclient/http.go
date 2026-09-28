package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		timeout = 10 * time.Minute
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
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
	t.setHeaders(req)

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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return fmt.Errorf("post %s: http %d: %s", t.url, resp.StatusCode, strings.TrimSpace(string(b)))
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
