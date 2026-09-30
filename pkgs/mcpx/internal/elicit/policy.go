package elicit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The fields mcpx's own questions use. Named constants because the answer is
// read back by name in a different package from the one that asked, and a
// typo in either place produces a question that can never be answered.
const (
	// FieldInstance is the enum of live instances a disambiguation offers.
	FieldInstance = "instance"
	// FieldConfirm is the boolean a destructive confirmation asks for. The
	// name matters: Route sends a lone confirmation boolean to a human,
	// because consent is not the agent's to give.
	FieldConfirm = "confirm"
)

// Choice is one option in a question mcpx raises about its own behaviour.
type Choice struct {
	// Value is what comes back in the answer.
	Value string
	// Title is the short label.
	Title string
	// Detail says what this option is currently doing, which is the whole
	// point of asking: "leased to session A, on the checkout page" is what
	// makes the choice possible.
	Detail string
}

// Ask is the shape of every question mcpx raises for itself.
//
// Two fields are not optional, and this type exists to make forgetting them
// impossible. A question with no deadline blocks a call forever. A question
// with no default cannot be resolved when the deadline passes, and headless
// callers -- CI, `opencode run`, anything driven by a plugin that is not
// watching -- are the common case rather than the exception.
type Ask struct {
	Server  string
	Tool    string
	Session string
	Message string
	Choices []Choice
	// Default is the value used when the deadline passes unanswered. It is
	// carried in the schema as well, so an answerer can see what will happen
	// if it says nothing.
	Default string
	TTL     time.Duration
}

// Disambiguation builds the question that asks which instance to use.
func Disambiguation(a Ask) Request {
	props := map[string]any{
		FieldInstance: map[string]any{
			"type":        "string",
			"description": "which live instance this call should use",
			"enum":        values(a.Choices),
			"enumNames":   titles(a.Choices),
			"default":     a.Default,
		},
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object", "properties": props, "required": []string{FieldInstance},
	})
	return Request{
		Server:    a.Server,
		Tool:      a.Tool,
		Session:   a.Session,
		Mode:      Form,
		Message:   a.Message + "\n" + describe(a.Choices),
		Schema:    schema,
		ExpiresAt: time.Now().Add(a.TTL),
	}
}

// Confirmation builds the question that asks whether a destructive call may
// proceed.
//
// The schema is a single boolean named confirm, which Route already sends to
// a human. That is deliberate rather than incidental: a confirmation is
// consent, and an agent confirming its own destructive call has confirmed
// nothing.
func Confirmation(a Ask) Request {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			FieldConfirm: map[string]any{
				"type":        "boolean",
				"description": "true to let the call proceed",
				"default":     a.Default == "true",
			},
		},
		"required": []string{FieldConfirm},
	})
	return Request{
		Server:    a.Server,
		Tool:      a.Tool,
		Session:   a.Session,
		Mode:      Form,
		Message:   a.Message,
		Schema:    schema,
		ExpiresAt: time.Now().Add(a.TTL),
	}
}

// Form builds a question asking for a set of values against a given schema,
// which is how a recipe's missing placeholders are collected.
func FormAsk(a Ask, schema json.RawMessage) Request {
	return Request{
		Server:    a.Server,
		Tool:      a.Tool,
		Session:   a.Session,
		Mode:      Form,
		Message:   a.Message,
		Schema:    schema,
		ExpiresAt: time.Now().Add(a.TTL),
	}
}

// SampleAsk builds a sampling request mcpx raises on its own behalf.
//
// The params are a CreateMessageRequest, exactly as an upstream server would
// have sent one, because the answerers already know that shape: the plugin,
// the agent answering through `mcpx elicit`, an MCP client that declared
// sampling. Inventing a second shape for mcpx's own requests would mean every
// answerer needed a second code path.
func SampleAsk(a Ask, params json.RawMessage) Request {
	return Request{
		Server:    a.Server,
		Session:   a.Session,
		Mode:      Sample,
		Message:   a.Message,
		Schema:    params,
		Audience:  ToAgent,
		Reason:    "sampling asks a model, and the agent driving mcpx has one",
		ExpiresAt: time.Now().Add(a.TTL),
	}
}

// Resolve reads one string field out of an answer, falling back to the
// default whenever the answer is not a usable accept.
//
// Decline, cancel and expiry all land here, and they all mean the same thing
// for a question mcpx asked itself: nobody chose, so the stated fallback is
// what happens. The boolean says whether anyone actually did choose, which is
// what a caller needs in order to log the difference.
func Resolve(ans Answer, field, fallback string) (string, bool) {
	if ans.Action != Accept || len(ans.Content) == 0 {
		return fallback, false
	}
	var doc map[string]any
	if json.Unmarshal(ans.Content, &doc) != nil {
		return fallback, false
	}
	v, ok := doc[field]
	if !ok {
		return fallback, false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	}
	return fmt.Sprint(v), true
}

func values(cs []Choice) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Value)
	}
	return out
}

func titles(cs []Choice) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Title)
	}
	return out
}

func describe(cs []Choice) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "  %s -- %s", c.Title, c.Detail)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
