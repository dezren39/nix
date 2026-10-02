package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// The era probe.
//
// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility
// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#backward-compatibility
//
// A dual-era client sends server/discover first and reads the answer:
// a DiscoverResult or a recognised modern error means modern, anything else
// -- any other error, a success that is not a DiscoverResult, silence, or an
// HTTP 4xx without a modern body -- means legacy. The fallback is keyed to
// "not recognisably modern", never to one error code, because legacy servers
// answer unknown pre-initialize requests however they like.

// Recognised modern JSON-RPC errors (schema/2026-07-28 HEADER_MISMATCH,
// MISSING_REQUIRED_CLIENT_CAPABILITY, UNSUPPORTED_PROTOCOL_VERSION).
const (
	codeHeaderMismatch          = -32020
	codeMissingClientCapability = -32021
	codeUnsupportedVersion      = -32022
)

func modernCode(code int) bool {
	return code == codeHeaderMismatch || code == codeMissingClientCapability || code == codeUnsupportedVersion
}

// ErrClosedDuringProbe is returned when a server closes the connection before
// answering server/discover. Some legacy servers exit on any first message
// that is not initialize, so the process is gone and nothing can be sent on
// it: the caller has to start a new one and connect legacy-only.
var ErrClosedDuringProbe = errors.New("the server closed the connection before answering server/discover")

// LegacyHTTPRefusedError is a Streamable HTTP endpoint that refused both the
// modern probe and a legacy initialize with a bare 400, 404 or 405. That is
// what a server that speaks only the deprecated HTTP+SSE transport does, so
// it is the signal to try that transport next.
type LegacyHTTPRefusedError struct {
	*HTTPStatusError
}

func (e *LegacyHTTPRefusedError) Unwrap() error { return e.HTTPStatusError }

// legacyHTTPRefusal converts an initialize failure into a
// LegacyHTTPRefusedError where it qualifies.
func legacyHTTPRefusal(err error) error {
	var he *HTTPStatusError
	if !errors.As(err, &he) || he.rpc != nil {
		return err
	}
	switch he.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed:
		return &LegacyHTTPRefusedError{he}
	}
	return err
}

// begin sends a request without waiting for its reply. The send itself runs
// in the background too, because the HTTP transport's Send blocks for the
// whole round trip -- and the probe needs to be able to give up on a reply
// without giving up on the request.
func (c *Client) begin(ctx context.Context, method string, params json.RawMessage) (int64, <-chan *rpcResponse) {
	return c.beginID(ctx, c.nextID.Add(1), method, params)
}

// beginID is begin with an id the caller allocated, for a caller that must
// know the id before any reply to it can arrive.
func (c *Client) beginID(ctx context.Context, id int64, method string, params json.RawMessage) (int64, <-chan *rpcResponse) {
	ch := make(chan *rpcResponse, 1)
	c.mu.Lock()
	if c.closed {
		err := c.recvErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("client closed")
		}
		ch <- &rpcResponse{sendErr: err}
		return id, ch
	}
	c.pending[id] = ch
	c.mu.Unlock()

	b, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		c.forget(id)
		ch <- &rpcResponse{sendErr: err}
		return id, ch
	}
	go func() {
		if err := c.t.Send(ctx, b); err != nil {
			c.mu.Lock()
			_, still := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if still {
				ch <- &rpcResponse{sendErr: err}
			}
		}
	}()
	return id, ch
}

// forget abandons a pending request; a late reply to it is dropped.
func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// verdict is what one reply to server/discover says about the server.
type verdict int

const (
	isModern      verdict = iota // a DiscoverResult with a mutual version
	isModernError                // modern, but nothing can be done about it
	retryVersion                 // modern, asked for a version it does not have
	isLegacy                     // anything not recognisably modern
	isClosed                     // the connection went away
	isFatal                      // not an answer about the era at all
)

// judge reads one reply to server/discover.
func (c *Client) judge(resp *rpcResponse, tried map[string]int) (verdict, string, error) {
	if resp.sendErr != nil {
		var he *HTTPStatusError
		if errors.As(resp.sendErr, &he) {
			if he.rpc != nil && modernCode(he.rpc.Code) {
				return c.judgeModernError(he.rpc, tried)
			}
			// A 4xx that is not a recognised modern error is how a legacy
			// Streamable HTTP server rejects a request it does not
			// understand. A 5xx says nothing about the era.
			if he.Status >= 400 && he.Status < 500 {
				return isLegacy, "", resp.sendErr
			}
		}
		// Connection refused, DNS, a closed pipe: nothing was learned, so
		// nothing may be concluded or cached.
		return isFatal, "", resp.sendErr
	}
	if resp.Error != nil {
		if !c.Alive() {
			return isClosed, "", ErrClosedDuringProbe
		}
		if modernCode(resp.Error.Code) {
			return c.judgeModernError(resp.Error, tried)
		}
		return isLegacy, "", resp.Error
	}
	var dr struct {
		SupportedVersions *[]string                  `json:"supportedVersions"`
		Capabilities      map[string]json.RawMessage `json:"capabilities"`
		Instructions      string                     `json:"instructions"`
		Meta              struct {
			ServerInfo ServerInfo `json:"io.modelcontextprotocol/serverInfo"`
		} `json:"_meta"`
	}
	if json.Unmarshal(resp.Result, &dr) != nil || dr.SupportedVersions == nil {
		// A legacy server with a catch-all handler answered under legacy
		// semantics. It is legacy; the answer means nothing.
		return isLegacy, "", fmt.Errorf("server/discover returned something that is not a DiscoverResult")
	}
	chosen := c.newestMutual(*dr.SupportedVersions)
	if chosen == "" {
		err := fmt.Errorf("no shared protocol version; the server offers %v and mcpx speaks %v",
			*dr.SupportedVersions, c.modernVersions)
		if offersLegacy(*dr.SupportedVersions) {
			return isLegacy, "", err
		}
		return isModernError, "", err
	}
	c.metaVersion = chosen
	c.Negotiated = chosen
	c.ServerInfo = dr.Meta.ServerInfo
	c.Capabilities = dr.Capabilities
	c.Instructions = dr.Instructions
	c.Era = EraModern
	c.modern.Store(true)
	return isModern, "", nil
}

// judgeModernError handles a recognised modern error. Only
// UnsupportedProtocolVersionError can be recovered from, by retrying with a
// version from its supported list: the newest one not yet tried, else --
// once -- one already tried that the server nonetheless lists. The versioning
// page says to "select a mutually supported version from the supported list
// and retry", and a server that rejects a version it lists may be failing
// transiently (the official suite simulates exactly that). Each version is
// sent at most twice, so a server that keeps doing it cannot loop us.
// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#protocol-version-negotiation
func (c *Client) judgeModernError(e *rpcError, tried map[string]int) (verdict, string, error) {
	if e.Code != codeUnsupportedVersion {
		return isModernError, "", fmt.Errorf("server/discover: %w", e)
	}
	var d struct {
		Supported []string `json:"supported"`
	}
	_ = json.Unmarshal(e.Data, &d)
	for round := 0; round < defaults.UpstreamVersionAttempts; round++ {
		for _, want := range c.modernVersions {
			if tried[want] != round {
				continue
			}
			for _, have := range d.Supported {
				if want == have {
					return retryVersion, want, nil
				}
			}
		}
	}
	err := fmt.Errorf("server/discover: no shared protocol version; the server offers %v and mcpx speaks %v (%w)",
		d.Supported, c.modernVersions, e)
	if offersLegacy(d.Supported) {
		return isLegacy, "", err
	}
	return isModernError, "", err
}

// offersLegacy reports whether a server's supported list names a revision
// mcpx negotiates through initialize. Such a server knows server/discover
// well enough to refuse it with UNSUPPORTED_PROTOCOL_VERSION, but the only
// versions it has are reached the old way -- Datadog's MCP server, on a
// 2025-11-25 SDK, answers exactly that. Only a list of versions mcpx speaks
// in neither era is the end of the road.
func offersLegacy(supported []string) bool {
	for _, v := range supported {
		if legacyVersion(v) {
			return true
		}
	}
	return false
}

func (c *Client) newestMutual(offered []string) string {
	for _, want := range c.modernVersions {
		for _, have := range offered {
			if want == have {
				return want
			}
		}
	}
	return ""
}

// probe settles the era modern-first. With allowLegacy false it is
// modern-only: whatever would have been a fallback is an error.
//
// On stdio, a discover that goes unanswered for probeTimeout gets an
// initialize sent alongside it rather than instead of it. Whichever answer
// settles the era first wins, so a legacy server that ignores unknown
// methods is reached after one timeout, while a modern server that is merely
// slow to start -- an npx download -- is still recognised when its
// DiscoverResult arrives late. Abandoning the discover at the timeout would
// misclassify exactly that server and cache the mistake.
func (c *Client) probe(ctx context.Context, allowLegacy bool) error {
	_, isHTTP := c.t.(*HTTPTransport)
	var timer <-chan time.Time
	if allowLegacy && !isHTTP {
		// HTTP decides by status code; a slow HTTP server is just slow.
		t := time.NewTimer(c.probeTimeout)
		defer t.Stop()
		timer = t.C
	}

	tried := map[string]int{}
	version := c.modernVersions[0]
	var (
		initID  int64
		initCh  <-chan *rpcResponse
		initErr error
	)
	stopInit := func() {
		if initCh != nil {
			c.forget(initID)
			initCh = nil
		}
	}
	for {
		tried[version]++
		params, err := c.withMeta(ctx, json.RawMessage(`{}`), version)
		if err != nil {
			return err
		}
		dID, dCh := c.begin(ctx, "server/discover", params)
		var legacyErr error
		retry := false
		for !retry && (dCh != nil || initCh != nil) {
			select {
			case <-ctx.Done():
				c.forget(dID)
				stopInit()
				if legacyErr != nil {
					return fmt.Errorf("server/discover: %v; initialize: %w", legacyErr, ctx.Err())
				}
				return fmt.Errorf("server/discover: %w", ctx.Err())

			case <-timer:
				timer = nil
				if initCh == nil && initErr == nil {
					initID, initCh = c.begin(ctx, "initialize", c.initializeParams())
				}

			case resp := <-dCh:
				dCh = nil
				v, next, err := c.judge(resp, tried)
				switch v {
				case isModern:
					stopInit()
					return nil
				case isModernError, isFatal, isClosed:
					stopInit()
					return err
				case retryVersion:
					stopInit()
					version, retry = next, true
				case isLegacy:
					if !allowLegacy {
						return fmt.Errorf("server/discover: %w", err)
					}
					legacyErr = err
					if initCh == nil {
						if initErr != nil {
							return fmt.Errorf("server/discover: %v; initialize: %w", err, initErr)
						}
						if ierr := c.initializeLegacy(ctx, c.clientName, c.clientVersion); ierr != nil {
							return fmt.Errorf("server/discover: %v; %w", err, ierr)
						}
						return nil
					}
				}

			case resp := <-initCh:
				initCh = nil
				if resp.sendErr == nil && resp.Error == nil {
					var ir initResult
					if err := json.Unmarshal(resp.Result, &ir); err == nil {
						c.forget(dID)
						return c.finishLegacy(ctx, ir)
					}
				}
				initErr = resp.sendErr
				if initErr == nil {
					if resp.Error != nil {
						initErr = resp.Error
					} else {
						initErr = errors.New("initialize returned an unreadable result")
					}
				}
				// Still waiting on discover: a modern server rejecting
				// initialize is exactly what this looks like.
				if dCh == nil {
					return fmt.Errorf("server/discover: %v; initialize: %w", legacyErr, initErr)
				}
			}
		}
		if !retry {
			// Unreachable while every verdict above returns; a silent
			// re-send of discover here would hide the bug that got here.
			return errors.New("server/discover: the probe ended without settling the era")
		}
	}
}

// legacyFirst tries initialize and, if that fails, the modern probe without
// its own fallback -- initialize has already been tried.
func (c *Client) legacyFirst(ctx context.Context) error {
	err := c.initializeLegacy(ctx, c.clientName, c.clientVersion)
	if err == nil {
		return nil
	}
	if !c.Alive() {
		return err
	}
	if merr := c.probe(ctx, false); merr != nil {
		// The first error is the one worth reporting: it came from the era
		// this server was expected to be.
		return fmt.Errorf("legacy handshake failed (%w); modern also failed (%v)", err, merr)
	}
	return nil
}
