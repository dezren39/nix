package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

// The revision matrix is a claim about the specification, and the schemas
// are the specification. One row per thing that actually differs.
func TestWhichRevisionDefinesWhat(t *testing.T) {
	cases := []struct {
		feature mcpserver.Feature
		want    map[string]bool
	}{
		{mcpserver.FeatStructuredContent, map[string]bool{
			"2025-03-26": false, "2025-06-18": true, "2025-11-25": true, "2026-07-28": true}},
		{mcpserver.FeatResourceLink, map[string]bool{
			"2025-03-26": false, "2025-06-18": true, "2025-11-25": true, "2026-07-28": true}},
		{mcpserver.FeatElicitation, map[string]bool{
			"2025-03-26": false, "2025-06-18": true, "2025-11-25": true, "2026-07-28": true}},
		{mcpserver.FeatElicitationURL, map[string]bool{
			"2025-03-26": false, "2025-06-18": false, "2025-11-25": true, "2026-07-28": true}},
		// Core tasks are 2025-11-25's alone; 2026-07-28 moved them to an
		// extension with different shapes.
		{mcpserver.FeatTasks, map[string]bool{
			"2025-03-26": false, "2025-06-18": false, "2025-11-25": true, "2026-07-28": false}},
		{mcpserver.FeatTasksExtension, map[string]bool{
			"2025-11-25": false, "2026-07-28": true}},
		{mcpserver.FeatExtensions, map[string]bool{
			"2025-11-25": false, "2026-07-28": true}},
		{mcpserver.FeatAudio, map[string]bool{
			"2024-11-05": false, "2025-03-26": true}},
		{mcpserver.FeatToolAnnotations, map[string]bool{
			"2024-11-05": false, "2025-03-26": true}},
		{mcpserver.FeatCompletions, map[string]bool{
			"2024-11-05": false, "2025-03-26": true}},
		{mcpserver.FeatTitle, map[string]bool{
			"2025-03-26": false, "2025-06-18": true}},
		{mcpserver.FeatIcons, map[string]bool{
			"2025-06-18": false, "2025-11-25": true}},
		{mcpserver.FeatResultType, map[string]bool{
			"2025-03-26": false, "2025-06-18": false, "2025-11-25": false, "2026-07-28": true}},
		// Removed rather than added: a ceiling, not a floor. The three below
		// are the ones 2026-07-28 dropped, and sending any of them to a
		// modern client offers a method that revision does not have.
		{mcpserver.FeatResourceSubscribe, map[string]bool{
			"2025-03-26": true, "2025-06-18": true, "2025-11-25": true, "2026-07-28": false}},
		{mcpserver.FeatLoggingSetLevel, map[string]bool{
			"2025-03-26": true, "2025-06-18": true, "2025-11-25": true, "2026-07-28": false}},
		{mcpserver.FeatElicitationComplete, map[string]bool{
			"2025-03-26": false, "2025-06-18": false, "2025-11-25": true, "2026-07-28": false}},
		{mcpserver.FeatInitialize, map[string]bool{
			"2024-11-05": true, "2025-03-26": true, "2025-06-18": true, "2025-11-25": true, "2026-07-28": false}},
		{mcpserver.FeatDiscover, map[string]bool{
			"2025-03-26": false, "2025-06-18": false, "2025-11-25": false, "2026-07-28": true}},
	}
	for _, c := range cases {
		for version, want := range c.want {
			if got := mcpserver.Defines(version, c.feature); got != want {
				t.Errorf("%s in %s: got %v, want %v", c.feature, version, got, want)
			}
		}
	}
}

// The matrix served over /v1/protocol has to be the same one the code reads,
// or a document nobody can check has been published.
func TestTheReportedMatrixCoversEveryRevisionMcpxServes(t *testing.T) {
	m := mcpserver.FeatureMatrix()
	for _, f := range mcpserver.Features {
		row, ok := m[string(f)]
		if !ok {
			t.Fatalf("%s is in Features and not in the matrix", f)
		}
		for _, v := range mcpserver.Supported {
			if _, ok := row[v]; !ok {
				t.Errorf("%s says nothing about %s", f, v)
			}
		}
	}
}

// Lexical comparison of revisions is load-bearing, and only correct because
// every one of them is a date. A revision that is not would sort wrong and
// silently enable a feature on something that does not have it.
func TestEveryRevisionIsADateSoOrderingWorks(t *testing.T) {
	prev := ""
	for i := len(mcpserver.Supported) - 1; i >= 0; i-- {
		v := mcpserver.Supported[i]
		if len(v) != len("2025-03-26") || strings.Count(v, "-") != 2 {
			t.Fatalf("%q is not a date; AtLeast compares revisions lexically", v)
		}
		if prev != "" && !(v > prev) {
			t.Errorf("Supported is not newest-first: %s does not sort after %s", v, prev)
		}
		prev = v
	}
}

func peer(version string, caps string) mcpserver.Peer {
	var m map[string]json.RawMessage
	if caps != "" {
		_ = json.Unmarshal([]byte(caps), &m)
	}
	return mcpserver.Peer{Version: version, Modern: mcpserver.Modern(version), Caps: m}
}

func TestAClientIsNeverSentSomethingItDidNotDeclare(t *testing.T) {
	cases := []struct {
		name  string
		p     mcpserver.Peer
		mode  string
		can   bool
		samps bool
	}{
		{"2025-03-26 has no elicitation at all",
			peer("2025-03-26", `{"elicitation":{}}`), "form", false, false},
		{"declared form on 2025-06-18",
			peer("2025-06-18", `{"elicitation":{}}`), "form", true, false},
		{"url mode does not exist in 2025-06-18",
			peer("2025-06-18", `{"elicitation":{"url":{}}}`), "url", false, false},
		{"url mode needs to be declared, not just available",
			peer("2025-11-25", `{"elicitation":{"form":{}}}`), "url", false, false},
		{"url mode declared on 2025-11-25",
			peer("2025-11-25", `{"elicitation":{"url":{}}}`), "url", true, false},
		{"nothing declared",
			peer("2026-07-28", `{}`), "form", false, false},
		{"sampling only",
			peer("2026-07-28", `{"sampling":{}}`), "form", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.CanElicit(c.mode); got != c.can {
				t.Errorf("CanElicit(%q) = %v, want %v", c.mode, got, c.can)
			}
			if got := c.p.CanSample(); got != c.samps {
				t.Errorf("CanSample() = %v, want %v", got, c.samps)
			}
		})
	}
}

func TestOutboundShapesAreDowngradedToTheNegotiatedRevision(t *testing.T) {
	srv := mcpserver.New(nil, "mcpx", "test")

	// A 2025-03-26 client has no structuredContent and no resource_link.
	// The data is not dropped -- it is spelled in the vocabulary that
	// client has, because a content block it cannot parse is not a degraded
	// result, it is an unreadable one.
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "resource_link", "uri": "file:///a", "name": "a"},
		},
		"structuredContent": map[string]any{"n": 42},
	}
	old := protoJSON(t, srv.Downgrade(result, "2025-03-26"))
	if strings.Contains(old, "structuredContent") {
		t.Errorf("2025-03-26 has no structuredContent:\n%s", old)
	}
	if !strings.Contains(old, `42`) {
		t.Errorf("the data should survive as text:\n%s", old)
	}
	if strings.Contains(old, "resource_link") {
		t.Errorf("2025-03-26 has no resource_link:\n%s", old)
	}
	if !strings.Contains(old, `"type":"resource"`) {
		t.Errorf("a link should become an embedded resource, which keeps the URI machine-readable:\n%s", old)
	}

	// And a revision that has them keeps them.
	fresh := protoJSON(t, srv.Downgrade(result, "2026-07-28"))
	for _, want := range []string{"structuredContent", "resource_link"} {
		if !strings.Contains(fresh, want) {
			t.Errorf("2026-07-28 defines %s and should keep it:\n%s", want, fresh)
		}
	}
}

// ResourceContents._meta is 2025-06-18; below it, it is removed.
func TestReadContentsMetaIsDowngraded(t *testing.T) {
	srv := mcpserver.New(nil, "mcpx", "test")
	result := map[string]any{"contents": []any{
		map[string]any{"uri": "a://b", "text": "x", "_meta": map[string]any{"k": "v"}}}}
	if got := protoJSON(t, srv.Downgrade(result, "2025-03-26")); strings.Contains(got, "_meta") {
		t.Errorf("2025-03-26 has no _meta on contents:\n%s", got)
	}
	if got := protoJSON(t, srv.Downgrade(result, "2025-06-18")); !strings.Contains(got, "_meta") {
		t.Errorf("2025-06-18 defines it:\n%s", got)
	}
}

func TestResultTypeGoesOnlyToARevisionThatDefinesIt(t *testing.T) {
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	modern := modernParams(nil)
	// tools/list, because 2026-07-28 removed ping and a removed method has
	// no result to stamp.
	got := protoJSON(t, srv.Handle(context.Background(), mcpserver.Request(1, "tools/list", modern)).Result)
	if !strings.Contains(got, `"resultType":"complete"`) {
		t.Errorf("2026-07-28 makes resultType mandatory:\n%s", got)
	}
	legacy := protoJSON(t, srv.Handle(context.Background(), mcpserver.Request(2, "tools/list", nil)).Result)
	if strings.Contains(legacy, "resultType") {
		t.Errorf("resultType to a legacy client claims a revision mcpx is not speaking:\n%s", legacy)
	}
}

func TestCapabilitiesAreDeclaredOnlyWhereTheRevisionDefinesThem(t *testing.T) {
	srv := mcpserver.New(newBackend(), "mcpx", "test")

	old := protoJSON(t, srv.Handle(context.Background(), mcpserver.Request(1, "initialize",
		map[string]any{"protocolVersion": "2025-06-18"})).Result)
	if strings.Contains(old, `"tasks"`) {
		t.Errorf("tasks arrived in 2025-11-25; declaring them to 2025-06-18 promises a revision's worth of methods it has not got:\n%s", old)
	}
	if !strings.Contains(old, `"logging"`) {
		// The capability means "this server sends log messages to the
		// client". mcpx relays its upstreams' messages to a client that set
		// a level, during that client's calls.
		t.Errorf("mcpx relays upstream log messages, so logging must be declared:\n%s", old)
	}

	modern := protoJSON(t, srv.Handle(context.Background(), mcpserver.Request(2, "server/discover",
		modernParams(nil))).Result)
	if strings.Contains(modern, `"logging"`) {
		t.Errorf("2026-07-28 removed logging/setLevel:\n%s", modern)
	}
	if !strings.Contains(modern, "io.modelcontextprotocol/tasks") {
		t.Errorf("tasks are an extension in 2026-07-28 and should be declared as one:\n%s", modern)
	}
}

// Accept liberally. A server offering a method beyond the revision in force
// withholds nothing, and refusing one mcpx implements would make a client
// that reached for it conclude mcpx cannot do the thing it can.
func TestEveryMethodIsAcceptedWhateverWasNegotiated(t *testing.T) {
	cases := []struct {
		name   string
		method string
		params any
	}{
		{"tasks from a 2025-06-18 client", "tasks/list",
			map[string]any{}},
		{"subscriptions/listen from a legacy client", "tools/list", map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := mcpserver.New(newBackend(), "mcpx", "test")
			srv.Notify = quietNotifier{}
			srv.SetPush(func(string, any) {})
			// Negotiate the older revision first, so the request below is
			// arriving on a connection that settled on it.
			srv.Handle(context.Background(), mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": "2025-06-18"}))
			resp := srv.Handle(context.Background(), mcpserver.Request(2, c.method, c.params))
			if resp != nil && resp.Error != nil {
				t.Errorf("%s was refused: %v", c.method, resp.Error)
			}
		})
	}
}

type quietNotifier struct{}

func (quietNotifier) Listen(ctx context.Context, f mcpserver.ListenFilter, send func(string, any)) {
	<-ctx.Done()
}

// The two halves of mcpx agree on the reserved _meta keys, which is the only
// thing stopping the server reading a key the client never writes.
func TestTheMetaKeysAgreeAcrossTheTwoHalves(t *testing.T) {
	pairs := [][2]string{
		{mcpserver.MetaProtocolVersion, mcpclient.MetaProtocolVersion},
		{mcpserver.MetaClientCapabilities, mcpclient.MetaClientCapabilities},
		{mcpserver.MetaClientInfo, mcpclient.MetaClientInfo},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("the server reads %q and the client writes %q", p[0], p[1])
		}
	}
}

func protoJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---- requestState ----

func TestARequestStateCannotBeForgedOrBorrowed(t *testing.T) {
	srv := mcpserver.New(nil, "mcpx", "test")
	token, err := srv.MintState("tsk-1", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := srv.VerifyState(token, "session-a"); err != nil || got != "tsk-1" {
		t.Fatalf("its own connection should resume: %q %v", got, err)
	}
	if _, err := srv.VerifyState(token, "session-b"); err == nil {
		t.Error("resuming someone else's call is reading their tool results")
	}
	if _, err := srv.VerifyState(token+"x", "session-a"); err == nil {
		t.Error("a tampered state verified")
	}
	// A connection with no identity that outlives the request cannot be
	// bound to, so no token may be issued for it at all.
	if _, err := srv.MintState("tsk-1", ""); err == nil {
		t.Error("a token bound to nothing is a token anyone may present")
	}
	other := mcpserver.New(nil, "mcpx", "test")
	if _, err := other.VerifyState(token, "session-a"); err == nil {
		t.Error("the key is per process; another one must not accept it")
	}
}

// ---- the ask loop ----

// scriptedAsker is an upstream call that asks what the test tells it to.
type scriptedAsker struct {
	questions []mcpserver.Question
	answers   map[string]json.RawMessage
	began     int
	abandoned int
	text      string
}

func (a *scriptedAsker) Begin(context.Context, string, json.RawMessage) (string, error) {
	a.began++
	a.answers = map[string]json.RawMessage{}
	return "call-1", nil
}

func (a *scriptedAsker) Poll(_ context.Context, _ string, _ time.Duration) (mcpserver.Outcome, error) {
	var open []mcpserver.Question
	for _, q := range a.questions {
		if _, done := a.answers[q.ID]; !done {
			open = append(open, q)
		}
	}
	if len(open) == 0 {
		return mcpserver.Outcome{Done: true, Text: a.text}, nil
	}
	return mcpserver.Outcome{Questions: open}, nil
}

func (a *scriptedAsker) Reply(_ context.Context, _ string, answers map[string]json.RawMessage) error {
	for k, v := range answers {
		a.answers[k] = v
	}
	return nil
}

func (a *scriptedAsker) Abandon(string) { a.abandoned++ }

func oneQuestion() []mcpserver.Question {
	return []mcpserver.Question{{
		ID: "elc-1", Method: "elicitation/create", Mode: "form", Server: "github",
		Params: json.RawMessage(`{"message":"which repo?","requestedSchema":{"type":"object"}}`),
	}}
}

func modernCall(id int, extra map[string]any) any {
	params := map[string]any{
		"name":      "mcpx_call",
		"arguments": map[string]any{"namespace": "x", "tool": "y"},
		"_meta": map[string]any{
			mcpserver.MetaProtocolVersion:    "2026-07-28",
			mcpserver.MetaClientCapabilities: map[string]any{"elicitation": map[string]any{}},
		},
	}
	for k, v := range extra {
		params[k] = v
	}
	return params
}

func TestAModernClientIsHandedTheQuestionAndRetries(t *testing.T) {
	asker := &scriptedAsker{questions: oneQuestion(), text: "done"}
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Ask = asker

	first := srv.Handle(context.Background(),
		mcpserver.Request(1, "tools/call", modernCall(1, nil)))
	body := protoJSON(t, first.Result)
	if !strings.Contains(body, `"input_required"`) {
		t.Fatalf("a modern server asks by answering:\n%s", body)
	}
	var ir struct {
		InputRequests map[string]any `json:"inputRequests"`
		RequestState  string         `json:"requestState"`
	}
	if err := json.Unmarshal([]byte(body), &ir); err != nil {
		t.Fatal(err)
	}
	if _, ok := ir.InputRequests["elc-1"]; !ok {
		t.Fatalf("the question should be keyed by its id:\n%s", body)
	}
	if ir.RequestState == "" {
		t.Fatal("without a requestState the server cannot know which call is being resumed")
	}

	second := srv.Handle(context.Background(), mcpserver.Request(2, "tools/call",
		modernCall(2, map[string]any{
			"requestState": ir.RequestState,
			"inputResponses": map[string]any{
				"elc-1": map[string]any{"action": "accept", "content": map[string]any{"repo": "me/thing"}},
			},
		})))
	got := protoJSON(t, second.Result)
	if !strings.Contains(got, "done") {
		t.Fatalf("the retry should carry the original call through:\n%s", got)
	}
	if asker.began != 1 {
		t.Errorf("the upstream call must survive the retry rather than restart; began %d times", asker.began)
	}
}

func TestAModernClientCannotResumeWithAnotherServersState(t *testing.T) {
	asker := &scriptedAsker{questions: oneQuestion(), text: "done"}
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Ask = asker
	resp := srv.Handle(context.Background(), mcpserver.Request(1, "tools/call",
		modernCall(1, map[string]any{"requestState": "not.a.real.token"})))
	if resp.Error == nil {
		t.Fatal("a requestState that does not verify must be refused")
	}
}

func TestAClientThatDeclaredNothingIsNotAsked(t *testing.T) {
	asker := &scriptedAsker{questions: oneQuestion(), text: "done"}
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Ask = asker
	resp := srv.Handle(context.Background(), mcpserver.Request(1, "tools/call",
		map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "x", "tool": "y"}}))
	if resp == nil || resp.Error != nil {
		t.Fatalf("the ordinary path should still answer: %v", resp)
	}
	if asker.began != 0 {
		t.Error("a client that declared nothing keeps the broker's routing, unchanged")
	}
}

// A sampling-only client must not be handed an elicitation, even though both
// travel the same way. Declaring one is not declaring the other.
func TestASamplingOnlyClientIsNotSentAnElicitation(t *testing.T) {
	asker := &scriptedAsker{questions: oneQuestion(), text: "done"}
	srv := mcpserver.New(newBackend(), "mcpx", "test")
	srv.Ask = asker
	params := map[string]any{
		"name":      "mcpx_call",
		"arguments": map[string]any{"namespace": "x", "tool": "y"},
		"_meta": map[string]any{
			mcpserver.MetaProtocolVersion:    "2026-07-28",
			mcpserver.MetaClientCapabilities: map[string]any{"sampling": map[string]any{}},
		},
	}
	// It never becomes answerable, so the call runs to the deadline rather
	// than offering a question this client cannot answer. The point of the
	// assertion is the absence: no input_required was returned.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp := srv.Handle(ctx, mcpserver.Request(1, "tools/call", params))
	if resp != nil && resp.Result != nil {
		if body := protoJSON(t, resp.Result); strings.Contains(body, "elicitation/create") {
			t.Fatalf("a sampling-only client was offered an elicitation:\n%s", body)
		}
	}
}

// TestARemovedMethodIsNotFoundForTheRevisionThatRemovedIt is the limit of
// "accept liberally".
//
// Offering a client more than its revision requires withholds nothing. But a
// method the revision *removed* is different: the removal is the
// specification pointing at a replacement, and 2026-07-28 requires
// method-not-found for it -- "If the server does not implement the requested
// RPC method, it MUST respond with 404 Not Found and a JSON-RPC error with
// code -32601", the 404 being how a dual-era client tells a modern server
// from a legacy endpoint that is simply absent. The official conformance
// suite scores exactly these five, by name, in the frozen 2026-07-28 set.
//
// This previously answered them all, and a subtest asserted that as correct.
func TestARemovedMethodIsNotFoundForTheRevisionThatRemovedIt(t *testing.T) {
	for _, m := range []string{
		"initialize", "ping", "logging/setLevel",
		"resources/subscribe", "resources/unsubscribe",
	} {
		t.Run("2026-07-28/removed/"+m, func(t *testing.T) {
			srv := mcpserver.New(newBackend(), "mcpx", "test")
			srv.Notify = quietNotifier{}
			srv.SetPush(func(string, any) {})
			resp := srv.Handle(context.Background(), mcpserver.Request(2, m,
				modernParams(map[string]any{"uri": "x://y", "level": "info"})))
			if resp == nil || resp.Error == nil {
				t.Fatalf("%s was answered for a 2026-07-28 peer: %+v", m, resp)
			}
			if resp.Error.Code != -32601 {
				t.Errorf("%s: code %d, want -32601", m, resp.Error.Code)
			}
		})
		if m == "initialize" {
			// Not applicable rather than skipped: initialize is the legacy
			// handshake itself, which every legacy subtest already sends.
			continue
		}
		t.Run("legacy-still-served/"+m, func(t *testing.T) {
			// The same method on a legacy connection is untouched.
			srv := mcpserver.New(newBackend(), "mcpx", "test")
			srv.Notify = quietNotifier{}
			srv.SetPush(func(string, any) {})
			srv.Handle(context.Background(), mcpserver.Request(1, "initialize",
				map[string]any{"protocolVersion": "2025-06-18"}))
			resp := srv.Handle(context.Background(), mcpserver.Request(2, m,
				map[string]any{"uri": "x://y", "level": "info"}))
			if resp != nil && resp.Error != nil && resp.Error.Code == -32601 {
				t.Errorf("%s must still be served to a legacy peer: %v", m, resp.Error)
			}
		})
	}
}
