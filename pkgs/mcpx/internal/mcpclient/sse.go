package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// LegacySSETransport speaks the HTTP+SSE transport of 2024-11-05: one GET
// opens an event stream whose first event, "endpoint", names the URL every
// client message is POSTed to; every server message, responses included,
// arrives on the stream.
//
// Deprecated by the protocol since 2025-03-26 and still what a good share of
// deployed remote servers speak. Every later revision tells a client that
// wants to reach them how: POST first, and on a 400, 404 or 405 without a
// modern body, GET expecting an endpoint event. The probe does the first
// half and reports LegacyHTTPRefusedError; this is the second.
//
// https://modelcontextprotocol.io/specification/2024-11-05/basic/transports#http-with-sse
// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#backwards-compatibility
type LegacySSETransport struct {
	url      string
	endpoint string
	headers  map[string]string
	hc       *http.Client

	incoming chan []byte
	closed   chan struct{}
	cancel   context.CancelFunc

	errMu sync.Mutex
	err   error
	once  sync.Once
}

// NewLegacySSE opens the event stream and waits for the endpoint event.
func NewLegacySSE(ctx context.Context, opts HTTPOptions) (*LegacySSETransport, error) {
	if opts.URL == "" {
		return nil, errors.New("http+sse transport: empty url")
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaults.HTTPRequestTimeout
	}
	sctx, cancel := context.WithCancel(context.Background())
	t := &LegacySSETransport{
		url: opts.URL, headers: opts.Headers, hc: &http.Client{Timeout: timeout},
		incoming: make(chan []byte, 64), closed: make(chan struct{}), cancel: cancel,
	}
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("http+sse: get %s: %w", opts.URL, err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.HTTPErrorBodyLimit))
		resp.Body.Close()
		cancel()
		return nil, &HTTPStatusError{URL: opts.URL, Status: resp.StatusCode, Body: bytes.TrimSpace(b)}
	}
	ready := make(chan string, 1)
	go t.read(resp.Body, ready)

	wait := time.NewTimer(defaults.UpstreamLegacyStreamEndpointTimeout)
	defer wait.Stop()
	select {
	case ep := <-ready:
		if ep == "" {
			t.Close()
			return nil, fmt.Errorf("http+sse: %s closed the stream without an endpoint event: %v", opts.URL, t.failure())
		}
		endpoint, err := sameOriginEndpoint(opts.URL, ep)
		if err != nil {
			t.Close()
			return nil, err
		}
		t.endpoint = endpoint
		return t, nil
	case <-wait.C:
		t.Close()
		return nil, fmt.Errorf("http+sse: %s sent no endpoint event within %s", opts.URL, defaults.UpstreamLegacyStreamEndpointTimeout)
	case <-ctx.Done():
		t.Close()
		return nil, ctx.Err()
	}
}

// sameOriginEndpoint resolves the endpoint event against the stream's URL
// and refuses one on another origin. The endpoint is where every message --
// and the configured credentials headers with it -- is sent, so a server
// that could name any URL could have mcpx post its credentials anywhere.
func sameOriginEndpoint(base, ep string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	e, err := b.Parse(strings.TrimSpace(ep))
	if err != nil {
		return "", fmt.Errorf("http+sse: endpoint %q: %w", ep, err)
	}
	if e.Scheme != b.Scheme || e.Host != b.Host {
		return "", fmt.Errorf("http+sse: endpoint %q is not on the origin of %s; refusing to send to it", ep, base)
	}
	return e.String(), nil
}

// read parses the event stream: the first endpoint event goes to ready,
// every message event to Recv.
func (t *LegacySSETransport) read(body io.ReadCloser, ready chan<- string) {
	defer body.Close()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var event string
	var data strings.Builder
	sentReady := false
	dispatch := func() {
		payload := data.String()
		name := event
		data.Reset()
		event = ""
		switch name {
		case "endpoint":
			if !sentReady {
				sentReady = true
				ready <- payload
			}
		case "", "message":
			if strings.TrimSpace(payload) == "" {
				return
			}
			select {
			case t.incoming <- []byte(payload):
			case <-t.closed:
			}
		}
	}
	for sc.Scan() {
		line := sc.Text()
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch {
		case line == "":
			if data.Len() > 0 || event != "" {
				dispatch()
			}
		case strings.HasPrefix(line, ":"):
		case field == "event":
			event = value
		case field == "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	t.setErr(fmt.Errorf("http+sse: %s: the event stream ended: %w", t.url, err))
	if !sentReady {
		ready <- ""
	}
	t.once.Do(func() { close(t.closed) })
}

func (t *LegacySSETransport) setErr(err error) {
	t.errMu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.errMu.Unlock()
}

func (t *LegacySSETransport) failure() error {
	t.errMu.Lock()
	defer t.errMu.Unlock()
	return t.err
}

// Send POSTs one message to the endpoint. The reply comes on the stream.
func (t *LegacySSETransport) Send(ctx context.Context, msg []byte) error {
	select {
	case <-t.closed:
		if err := t.failure(); err != nil {
			return err
		}
		return errors.New("http+sse transport closed")
	default:
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	resp, err := t.hc.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", t.endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, defaults.HTTPErrorBodyLimit))
		return &HTTPStatusError{URL: t.endpoint, Status: resp.StatusCode, Body: bytes.TrimSpace(b)}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, defaults.HTTPErrorBodyLimit))
	return nil
}

// Recv returns the next message from the stream.
func (t *LegacySSETransport) Recv() ([]byte, error) {
	select {
	case b := <-t.incoming:
		return b, nil
	case <-t.closed:
		select {
		case b := <-t.incoming:
			return b, nil
		default:
		}
		if err := t.failure(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
}

// Close ends the stream.
func (t *LegacySSETransport) Close() error {
	t.setErr(errors.New("http+sse transport closed"))
	t.cancel()
	t.once.Do(func() { close(t.closed) })
	return nil
}

// Info describes the endpoint.
func (t *LegacySSETransport) Info() string { return t.url + " (http+sse)" }
