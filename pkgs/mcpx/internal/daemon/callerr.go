package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/settings"
)

type callSetKey struct{}

// withCallSettings carries a request's call settings -- the daemon's live
// set with the caller's header on top -- to the registry, which otherwise
// sees only the daemon's.
func withCallSettings(ctx context.Context, cs *settings.Set) context.Context {
	return context.WithValue(ctx, callSetKey{}, cs)
}

// repairAutonomy is the request's repair.autonomy, already lowered to
// autonomy.max, or the daemon's own when the call came without a request.
func (r *Registry) repairAutonomy(ctx context.Context) string {
	if cs, ok := ctx.Value(callSetKey{}).(*settings.Set); ok && cs != nil {
		return cs.String("repair.autonomy")
	}
	if r.set != nil {
		return r.set.String("repair.autonomy")
	}
	return "advise"
}

// A call a server refused is explained here, in the registry, because the
// surfaces reach a tool through Registry.Call: /v1/call, the path-per-tool
// route, a call run as a task, a script's generated client, `mcpx call`, and
// mcpx_call over MCP. Explaining it once is what keeps those from giving six
// different answers to one failure. The exception is CallAsk
// (routes_proto.go), which leases the pool itself and so is not explained
// until it calls explainCall too.

// CallErrorBody is what /v1 answers a failed call with.
//
// Error is the text every caller already reads, now with the rendered
// diagnostics after the server's own message. Diagnostics is the same
// finding as data, for a caller that would rather act on a field than parse
// a sentence -- which is also what a repair step needs to start from.
type CallErrorBody struct {
	Error       string                `json:"error"`
	Diagnostics []diagnose.Diagnostic `json:"diagnostics,omitempty"`
}

// callFailure is an upstream refusal together with what mcpx worked out
// about it.
//
// The server's error stays first and whole. A diagnostic is an inference
// from the catalog; the server's message is the one fact about the failure
// that was not inferred, so it is added to rather than replaced.
type callFailure struct {
	err         error
	diagnostics []diagnose.Diagnostic
}

func (f *callFailure) Error() string {
	return f.err.Error() + "\n" + diagnose.Render(f.diagnostics)
}

func (f *callFailure) Unwrap() error { return f.err }

// explainCall attaches diagnostics to a failed call when the catalog can say
// something the server did not.
//
// That is repair at advise on the autonomy dial; repair.autonomy off, from
// the caller or lowered to it by autonomy.max, returns the server's error
// alone.
func (r *Registry) explainCall(ctx context.Context, server, tool string, args any, err error) error {
	if !settings.AutonomyAtLeast(r.repairAutonomy(ctx), "advise") {
		return err
	}
	code, message, ok := upstreamFault(err)
	if !ok {
		// A timeout, a server that would not start, a pipe that broke before
		// the answer: nothing came back, so there is nothing to explain, and
		// the arguments are the one thing that was not at fault.
		return err
	}
	ns := server
	if v, found := r.View(server); found && v.Namespace != "" {
		ns = v.Namespace
	}
	cat := r.DiagnoseCatalog()
	if !catalogHas(cat, ns) {
		// Nothing has been read from this server yet. Against an empty
		// catalog every tool is unknown, and saying so would be false.
		return err
	}
	sent, _ := json.Marshal(args)
	ds := diagnose.CallError(diagnose.CallErrorInput{
		Namespace: ns, Tool: tool, Args: sent, Code: code, Message: message,
	}, cat)
	if len(ds) == 0 {
		return err
	}
	return &callFailure{err: err, diagnostics: ds}
}

func catalogHas(cat diagnose.Catalog, ns string) bool {
	for _, t := range cat.Tools {
		if t.Namespace == ns {
			return true
		}
	}
	return false
}

// callDiagnostics is what explainCall found, if anything.
func callDiagnostics(err error) []diagnose.Diagnostic {
	var f *callFailure
	if errors.As(err, &f) {
		return f.diagnostics
	}
	return nil
}

func callErrorBody(err error) CallErrorBody {
	return CallErrorBody{Error: err.Error(), Diagnostics: callDiagnostics(err)}
}

// mcpclientPkg identifies the package whose errors are a server's answer.
var mcpclientPkg = reflect.TypeOf(mcpclient.Tool{}).PkgPath()

// upstreamFault is the JSON-RPC error a server answered with, when that is
// what err is.
//
// mcpclient does not export its error type, so the code and message are
// read by reflection, and only from a type declared in that package: a
// daemon-side error with a Code field (tasks.Fault carries an HTTP status
// there) must never pass for a server's answer. The alternative, parsing
// "mcp error -32602: ..." back out of the text, depends on the same package
// more loosely and fails more quietly. The test in callerr_test.go drives a
// real mcpclient.Client, so a change on that side fails there rather than
// silently switching this off.
func upstreamFault(err error) (code int, message string, ok bool) {
	for e := err; e != nil; e = errors.Unwrap(e) {
		v := reflect.ValueOf(e)
		if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
			continue
		}
		s := v.Elem()
		if s.Type().PkgPath() != mcpclientPkg {
			continue
		}
		c, m := s.FieldByName("Code"), s.FieldByName("Message")
		if c.Kind() != reflect.Int || m.Kind() != reflect.String {
			continue
		}
		return int(c.Int()), m.String(), true
	}
	return 0, "", false
}
