package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// daemonAsker runs a request the upstream server may interrupt.
//
// It is the bridge between the two halves of the question: mcpx's MCP server
// knows a client that can answer one, and the daemon knows the call that
// raised it. Neither can see the other, so this sits between them and speaks
// /v1 -- the same /v1 a curl user would, which is what keeps the two
// surfaces honest about each other.
type daemonAsker struct{ app *App }

func (d daemonAsker) client(ctx context.Context) (*Client, error) {
	return d.app.ensure(ctx)
}

// callContextOf is the session a call made through the MCP server belongs
// to. One per connection would be better and the protocol gives no way to
// learn it, so one per process is the honest answer.
func (d daemonAsker) callContextOf() config.CallContext {
	s := d.app.mcpSession()
	return d.app.callContext(s, s)
}

// Begin translates one of mcpx's own tools into the upstream call behind it.
//
// Only `mcpx_call` and the two pass-through methods reach a server that
// could ask anything. Everything else -- a catalogue read, a script, a /v1
// operation -- answers ErrNotInterruptible and is run the ordinary way.
func (d daemonAsker) Begin(ctx context.Context, kind string, params json.RawMessage) (string, error) {
	body := map[string]any{"kind": kind, "context": d.callContextOf()}

	switch kind {
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments struct {
				Namespace string          `json:"namespace"`
				Tool      string          `json:"tool"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Name != "mcpx_call" {
			return "", mcpserver.ErrNotInterruptible
		}
		ns, tool := p.Arguments.Namespace, p.Arguments.Tool
		if tool == "" {
			// A dotted name in the namespace field is what somebody sends,
			// because that is how the tools are written everywhere else.
			if a, b, ok := strings.Cut(ns, "."); ok {
				ns, tool = a, b
			}
		}
		if ns == "" || tool == "" {
			return "", mcpserver.ErrNotInterruptible
		}
		body["server"], body["tool"] = ns, tool
		if len(p.Arguments.Arguments) > 0 {
			body["args"] = p.Arguments.Arguments
		}

	case "prompts/get":
		var p struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return "", mcpserver.ErrNotInterruptible
		}
		server, name, err := d.resolvePrompt(ctx, p.Name)
		if err != nil {
			return "", err
		}
		body["server"], body["name"], body["arguments"] = server, name, p.Arguments

	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return "", mcpserver.ErrNotInterruptible
		}
		ns, rest, ok := strings.Cut(strings.TrimPrefix(p.URI, "mcpx://"), "/")
		if !ok {
			return "", mcpserver.ErrNotInterruptible
		}
		body["server"], body["uri"] = ns, rest

	default:
		return "", mcpserver.ErrNotInterruptible
	}

	c, err := d.client(ctx)
	if err != nil {
		return "", err
	}
	raw, err := c.do(ctx, http.MethodPost, "/v1/ask", body)
	if err != nil {
		return "", err
	}
	var out struct {
		CallID string `json:"callId"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.CallID == "" {
		return "", fmt.Errorf("the daemon started a call but named no id")
	}
	return out.CallID, nil
}

// resolvePrompt maps a namespaced prompt name back to its server.
func (d daemonAsker) resolvePrompt(ctx context.Context, name string) (string, string, error) {
	c, err := d.client(ctx)
	if err != nil {
		return "", "", err
	}
	list, err := c.Prompts(ctx, nil)
	if err != nil {
		return "", "", err
	}
	for _, p := range list {
		if p.Namespace+"_"+p.Name == name || p.Name == name {
			return p.Namespace, p.Name, nil
		}
	}
	return "", "", fmt.Errorf("no prompt named %q", name)
}

type askPollReply struct {
	Status    string                     `json:"status"`
	Done      bool                       `json:"done"`
	Error     string                     `json:"error"`
	Questions []mcpserver.Question       `json:"questions"`
	Result    map[string]json.RawMessage `json:"result"`
}

func (d daemonAsker) Poll(ctx context.Context, callID string, wait time.Duration) (mcpserver.Outcome, error) {
	c, err := d.client(ctx)
	if err != nil {
		return mcpserver.Outcome{}, err
	}
	ms := int(wait / time.Millisecond)
	if ms < 1 {
		ms = 1
	}
	raw, err := c.do(ctx, http.MethodGet,
		"/v1/ask/"+callID+"?waitMs="+strconv.Itoa(ms), nil)
	if err != nil {
		return mcpserver.Outcome{}, err
	}
	var reply askPollReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return mcpserver.Outcome{}, err
	}
	out := mcpserver.Outcome{Done: reply.Done, Questions: reply.Questions}
	if !reply.Done {
		return out, nil
	}
	if reply.Error != "" {
		// A failed upstream call is a tool result with isError, not a
		// protocol error: a client that retries the wrong thing on a tool
		// failure never converges.
		out.Text, out.IsError = reply.Error, true
		return out, nil
	}
	out.Text, out.MimeType = renderAsk(reply.Result)
	return out, nil
}

// renderAsk turns the daemon's task result into the text every other mcpx
// tool result is, using the same renderers the direct path uses. Two ways to
// render one result is two ways for them to disagree.
func renderAsk(result map[string]json.RawMessage) (string, string) {
	var kind string
	_ = json.Unmarshal(result["kind"], &kind)
	inner := result["result"]
	switch kind {
	case "prompts/get":
		return renderPrompt(inner), ""
	case "resources/read":
		text, mime, err := renderResource(inner)
		if err != nil {
			return string(inner), ""
		}
		return text, mime
	default:
		return renderResult(inner), ""
	}
}

func (d daemonAsker) Reply(ctx context.Context, callID string, answers map[string]json.RawMessage) error {
	c, err := d.client(ctx)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/v1/ask/"+callID+"/answers",
		map[string]any{"answers": answers})
	return err
}

func (d daemonAsker) Abandon(callID string) {
	// Best effort, on its own deadline: the request that wanted this is
	// already being failed, and its context is cancelled or about to be.
	ctx, cancel := context.WithTimeout(context.Background(), defaults.ProtoAbandonGrace)
	defer cancel()
	if c, err := d.client(ctx); err == nil {
		_, _ = c.do(ctx, http.MethodPost, "/v1/ask/"+callID+"/abandon", nil)
	}
}

// lazyMCP builds mcpx's MCP server the first time something asks for it.
//
// Built lazily because constructing it reads adapter declarations and may
// fetch a declared OpenAPI document over the network. The daemon auto-starts
// on the first command anybody runs, and a remote document that is slow to
// answer would become a daemon that is slow to start -- for a surface that
// particular invocation is not using.
type lazyMCP struct {
	app  *App
	once sync.Once
	srv  *mcpserver.Server
	err  error
}

func (l *lazyMCP) get(ctx context.Context) (*mcpserver.Server, error) {
	l.once.Do(func() { l.srv, l.err = l.app.MCPServer(ctx) })
	return l.srv, l.err
}

func (l *lazyMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv, err := l.get(r.Context())
	if err != nil {
		http.Error(w, "mcpx could not build its MCP surface: "+err.Error(),
			http.StatusInternalServerError)
		return
	}
	srv.ServeHTTP(w, r)
}

// InvokeTool runs one tool, for the plain-POST projection of the surface.
func (l *lazyMCP) InvokeTool(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	srv, err := l.get(ctx)
	if err != nil {
		return "", err
	}
	return srv.InvokeTool(ctx, tool, args)
}
