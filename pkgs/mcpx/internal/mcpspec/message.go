package mcpspec

import (
	"fmt"
	"sort"
	"strings"
)

// Direction says who sent a frame; it picks which request/notification definitions apply.
type Direction int

const (
	FromServer Direction = iota
	FromClient
)

// MethodDef returns the definition for the request or notification with this method, found by the
// "method" const in the schema rather than a hand-kept table, so every revision answers for itself.
// Union wrappers such as 2026-07-28's ClientNotification also carry a method const; they are skipped.
func (s *Schema) MethodDef(method string) (string, bool) {
	var hits []string
	for name, d := range s.Defs {
		m, _ := d.(map[string]any)
		props, _ := m["properties"].(map[string]any)
		mp, _ := props["method"].(map[string]any)
		if c, _ := mp["const"].(string); c == method &&
			!strings.HasPrefix(name, "Client") && !strings.HasPrefix(name, "Server") {
			hits = append(hits, name)
		}
	}
	sort.Strings(hits)
	if len(hits) == 0 {
		return "", false
	}
	return hits[0], true
}

// ResultDef names what a successful response to method must match, and whether that definition is the
// whole response envelope. 2026-07-28 publishes per-method envelopes (CallToolResultResponse) whose result is
// a union with InputRequiredResult; those are preferred. Otherwise the XRequest -> XResult naming the schema
// uses throughout, then EmptyResult (ping, logging/setLevel, resources/subscribe), then plain Result.
func (s *Schema) ResultDef(method string) (def string, envelope bool, err error) {
	req, ok := s.MethodDef(method)
	if !ok {
		return "", false, fmt.Errorf("%s defines no method %q", s.Rev, method)
	}
	base := strings.TrimSuffix(req, "Request")
	if _, ok := s.Defs[base+"ResultResponse"]; ok {
		return base + "ResultResponse", true, nil
	}
	for _, n := range []string{base + "Result", "EmptyResult", "Result"} {
		if _, ok := s.Defs[n]; ok {
			return n, false, nil
		}
	}
	return "", false, fmt.Errorf("%s has no result definition for %q", s.Rev, method)
}

func (s *Schema) first(names ...string) string {
	for _, n := range names {
		if _, ok := s.Defs[n]; ok {
			return n
		}
	}
	return names[0]
}

// ErrExtension marks a frame the core schema cannot judge because it belongs to an extension (2026-07-28
// moved tasks out of the core: a result with resultType "task" and the tasks/* methods are defined in
// ext-tasks, which is not vendored here). Callers decide whether that is acceptable.
type ErrExtension struct{ What string }

func (e *ErrExtension) Error() string { return "not in the core schema (extension): " + e.What }

// ValidateServerMessage checks a frame a server sent. requestMethod is the method of the client request a
// response answers (ignored for requests and notifications).
func ValidateServerMessage(rev string, frame []byte, requestMethod string) error {
	return validateMessage(rev, frame, requestMethod, false)
}

// ValidateServerMessageStrict also rejects properties the revision does not define. The schemas leave
// objects open so receivers tolerate newer peers; a sender that uses that openness to send keys from
// another revision is breaking "send conservatively", which only strict mode can see.
func ValidateServerMessageStrict(rev string, frame []byte, requestMethod string) error {
	return validateMessage(rev, frame, requestMethod, true)
}

// ValidateClientMessage checks a frame a client sent. Responses from a client (to sampling, elicitation,
// roots) need the server request's method; pass it as requestMethod, or "" for requests and notifications.
func ValidateClientMessage(rev string, frame []byte, requestMethod ...string) error {
	m := ""
	if len(requestMethod) > 0 {
		m = requestMethod[0]
	}
	return validateMessage(rev, frame, m, false)
}

// ValidateClientMessageStrict is ValidateClientMessage that also rejects properties the revision does not
// define: the check that a client sends a server only what the negotiated revision has.
func ValidateClientMessageStrict(rev string, frame []byte, requestMethod ...string) error {
	m := ""
	if len(requestMethod) > 0 {
		m = requestMethod[0]
	}
	return validateMessage(rev, frame, m, true)
}

func validateMessage(rev string, frame []byte, requestMethod string, strict bool) error {
	s, err := Get(rev)
	if err != nil {
		return err
	}
	doc, err := decode(frame)
	if err != nil {
		return &Error{Path: "$", Msg: "invalid JSON: " + err.Error()}
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		// JSON-RPC batches existed only in 2025-03-26 and are not something mcpx sends.
		return &Error{Path: "$", Msg: "frame is not a JSON object"}
	}
	if method, ok := obj["method"].(string); ok {
		_, hasID := obj["id"]
		envelope := s.first("JSONRPCRequest")
		if !hasID {
			envelope = "JSONRPCNotification"
		}
		if err := s.validateAt(envelope, doc, "$", false); err != nil {
			return err
		}
		def, ok := s.MethodDef(method)
		if !ok {
			if strings.HasPrefix(method, "tasks/") || strings.HasPrefix(method, "notifications/tasks/") {
				return &ErrExtension{What: method}
			}
			return &Error{Path: "$.method", Msg: fmt.Sprintf("%q is not a %s method", method, rev)}
		}
		return s.validateAt(def, doc, "$", strict)
	}
	if _, ok := obj["error"]; ok {
		return s.validateAt(s.first("JSONRPCErrorResponse", "JSONRPCError"), doc, "$", strict)
	}
	if err := s.validateAt(s.first("JSONRPCResultResponse", "JSONRPCResponse"), doc, "$", false); err != nil {
		return err
	}
	if requestMethod == "" {
		return nil
	}
	result, _ := obj["result"].(map[string]any)
	// A task-augmented request (2025-11-25) is answered with CreateTaskResult instead of the method's own
	// result; the "task" member is what tells them apart on the wire.
	if _, isTask := result["task"]; isTask && s.Defs["CreateTaskResult"] != nil {
		return s.validateAt("CreateTaskResult", obj["result"], "$.result", strict)
	}
	// 2026-07-28 envelopes are anyOf(InputRequiredResult, XResult), and InputRequiredResult requires only
	// resultType, so the union admits almost anything. resultType is the discriminator the spec defines;
	// dispatch on it instead of on the union.
	if rt, ok := result["resultType"].(string); ok {
		switch rt {
		case "input_required":
			return s.validateAt("InputRequiredResult", obj["result"], "$.result", strict)
		case "task":
			return &ErrExtension{What: `resultType "task" for ` + requestMethod}
		}
	}
	if strings.HasPrefix(requestMethod, "tasks/") && !hasMethod(s, requestMethod) {
		return &ErrExtension{What: requestMethod + " result"}
	}
	def, err := s.resultDef(requestMethod)
	if err != nil {
		return err
	}
	return s.validateAt(def, obj["result"], "$.result", strict)
}

func hasMethod(s *Schema, m string) bool { _, ok := s.MethodDef(m); return ok }

// resultDef is ResultDef without the envelope: the XResult the complete answer must match.
func (s *Schema) resultDef(method string) (string, error) {
	req, ok := s.MethodDef(method)
	if !ok {
		return "", fmt.Errorf("%s defines no method %q", s.Rev, method)
	}
	base := strings.TrimSuffix(req, "Request")
	for _, n := range []string{base + "Result", "EmptyResult", "Result"} {
		if _, ok := s.Defs[n]; ok {
			return n, nil
		}
	}
	return "", fmt.Errorf("%s has no result definition for %q", s.Rev, method)
}
