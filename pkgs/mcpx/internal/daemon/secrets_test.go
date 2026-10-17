package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// secretsRig is a daemon with one server, demo, that requires the given
// secrets. demo's command leaves a marker file behind if it is ever started, so
// a test can tell a refused call from one that ran: the marker only exists in
// the second case.
type secretsRig struct {
	srv    *Server
	mux    *http.ServeMux
	logs   *bytes.Buffer
	marker string
}

func newSecretsRig(t *testing.T, requires ...string) *secretsRig {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	logs := &bytes.Buffer{}
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"demo": {
			Name:    "demo",
			Command: "/bin/sh",
			Args:    []string{"-c", "touch " + marker + "; exit 1"},
			Mcpx:    &config.Extras{RequiresSecrets: requires},
		},
	}}
	paths := Paths{State: dir, Cache: dir, Socket: filepath.Join(dir, "d.sock"), Info: filepath.Join(dir, "d.json")}
	srv, err := NewServer(Options{Config: cfg, Paths: paths, Version: "test", Logger: log.New(logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.reg.Close)
	mux := http.NewServeMux()
	srv.routes(mux)
	return &secretsRig{srv: srv, mux: mux, logs: logs, marker: marker}
}

func (g *secretsRig) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	g.mux.ServeHTTP(rec, req)
	return rec
}

func (g *secretsRig) started() bool {
	_, err := os.Stat(g.marker)
	return err == nil
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

// envName is a variable no test environment sets, so "missing" is what these
// tests start from.
const envName = "MCPX_SECRETS_TEST_TOKEN"

func TestServerStatusFieldsFollowTheSecrets(t *testing.T) {
	g := newSecretsRig(t, envName)

	rec := g.do(t, http.MethodGet, "/v1/servers/demo", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/servers/demo = %d: %s", rec.Code, rec.Body)
	}
	s := decode[serverRecord](t, rec)
	if strings.Join(s.RequiresSecrets, ",") != envName || strings.Join(s.MissingSecrets, ",") != envName || s.Active {
		t.Fatalf("a server missing its secret is inactive and says what it lacks: %+v", s)
	}

	list := decode[struct {
		Servers []serverRecord `json:"servers"`
	}](t, g.do(t, http.MethodGet, "/v1/servers", ""))
	if len(list.Servers) != 1 || list.Servers[0].Active {
		t.Fatalf("GET /v1/servers carries the same status: %+v", list.Servers)
	}

	if rec := g.do(t, http.MethodGet, "/v1/servers/nothing-here", ""); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown server is 404, got %d", rec.Code)
	}
}

func TestEmptyRequiredListIsAnArrayNotNull(t *testing.T) {
	g := newSecretsRig(t)
	body := g.do(t, http.MethodGet, "/v1/servers/demo", "").Body.String()
	for _, key := range []string{`"requiresSecrets":[]`, `"missingSecrets":[]`, `"active":true`} {
		if !strings.Contains(body, key) {
			t.Errorf("a server with no requirements says %s: %s", key, body)
		}
	}
}

func TestPutReactivatesAndDeleteDeactivates(t *testing.T) {
	g := newSecretsRig(t, envName)

	put := g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":"tok-live"}`)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", put.Code, put.Body)
	}
	if got := decode[map[string]any](t, put); got["name"] != envName || got["set"] != true || got["source"] != "runtime" {
		t.Fatalf("PUT answers name, set and source: %v", got)
	}
	if s := decode[serverRecord](t, g.do(t, http.MethodGet, "/v1/servers/demo", "")); !s.Active || len(s.MissingSecrets) != 0 {
		t.Fatalf("after PUT the server is active and missing nothing: %+v", s)
	}

	del := g.do(t, http.MethodDelete, "/v1/secrets/"+envName, "")
	if del.Code != http.StatusOK {
		t.Fatalf("DELETE = %d: %s", del.Code, del.Body)
	}
	if got := decode[map[string]any](t, del); got["set"] != false {
		t.Fatalf("with nothing in the environment, DELETE leaves it unset: %v", got)
	}
	if s := decode[serverRecord](t, g.do(t, http.MethodGet, "/v1/servers/demo", "")); s.Active {
		t.Fatalf("after DELETE the server waits again: %+v", s)
	}
}

func TestSecretValueNeverAppearsInAnyAnswerOrLog(t *testing.T) {
	const secret = "tok-7f3a-should-never-appear"
	g := newSecretsRig(t, envName)

	// Every answer on a secrets route, the good and the refused. A refusal
	// that quoted its input would be the way a value leaked.
	secretRoutes := map[*httptest.ResponseRecorder]bool{}
	answer := func(rec *httptest.ResponseRecorder, onSecretsRoute bool) *httptest.ResponseRecorder {
		secretRoutes[rec] = onSecretsRoute
		return rec
	}
	answers := []*httptest.ResponseRecorder{
		answer(g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":"`+secret+`"}`), true),
		answer(g.do(t, http.MethodGet, "/v1/secrets", ""), true),
		answer(g.do(t, http.MethodGet, "/v1/servers/demo", ""), false),
		answer(g.do(t, http.MethodGet, "/v1/servers", ""), false),
		answer(g.do(t, http.MethodPost, "/v1/preflight", `{"source":"await tools.demo.x()"}`), true),
		answer(g.do(t, http.MethodGet, "/v1/secret-requests", ""), true),
		answer(g.do(t, http.MethodPut, "/v1/secrets/bad-name", `{"value":"`+secret+`"}`), true),
		answer(g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":"`+secret), true),
		answer(g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":5,"junk":"`+secret+`"}`), true),
		answer(g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":""}`), true),
		answer(g.do(t, http.MethodDelete, "/v1/secrets/"+envName, ""), true),
	}
	for i, rec := range answers {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("answer %d carries the value: %s", i, rec.Body)
		}
		if secretRoutes[rec] && rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("answer %d on a secrets route is not no-store: %q", i, rec.Header().Get("Cache-Control"))
		}
	}
	if strings.Contains(g.logs.String(), secret) {
		t.Errorf("the daemon's log carries the value:\n%s", g.logs)
	}
	if !strings.Contains(g.logs.String(), "secret "+envName) {
		t.Errorf("the log names the secret that changed, so an operator can see it:\n%s", g.logs)
	}
}

func TestSecretsRoutesRefuseBadInputWithoutEchoingIt(t *testing.T) {
	g := newSecretsRig(t, envName)
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"empty value", http.MethodPut, "/v1/secrets/" + envName, `{"value":""}`, http.StatusBadRequest},
		{"missing value", http.MethodPut, "/v1/secrets/" + envName, `{}`, http.StatusBadRequest},
		{"null value", http.MethodPut, "/v1/secrets/" + envName, `{"value":null}`, http.StatusBadRequest},
		{"a number for a value", http.MethodPut, "/v1/secrets/" + envName, `{"value":5}`, http.StatusBadRequest},
		{"not an environment name", http.MethodPut, "/v1/secrets/1BAD", `{"value":"x"}`, http.StatusBadRequest},
		{"a dash is not a name", http.MethodPut, "/v1/secrets/dash-name", `{"value":"x"}`, http.StatusBadRequest},
		{"unknown request", http.MethodGet, "/v1/secret-requests/sr-none", "", http.StatusNotFound},
		{"decline unknown", http.MethodDelete, "/v1/secret-requests/sr-none", "", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := g.do(t, c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Fatalf("%s = %d; want %d: %s", c.name, rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestSecretListNamesOnlyAndSaysWhereAValueComes(t *testing.T) {
	const fromEnv = "MCPX_SECRETS_TEST_FROM_ENV"
	t.Setenv(fromEnv, "env-value")
	g := newSecretsRig(t, envName, fromEnv)
	if rec := g.do(t, http.MethodPut, "/v1/secrets/MCPX_SECRETS_TEST_RUNTIME_ONLY", `{"value":"r"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT runtime-only = %d", rec.Code)
	}

	rec := g.do(t, http.MethodGet, "/v1/secrets", "")
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("the secrets listing is not cacheable: %q", rec.Header().Get("Cache-Control"))
	}
	type row struct {
		Name   string  `json:"name"`
		Set    bool    `json:"set"`
		Source *string `json:"source"`
	}
	by := map[string]row{}
	for _, r := range decode[struct {
		Secrets []row `json:"secrets"`
	}](t, rec).Secrets {
		by[r.Name] = r
	}
	if r := by[envName]; r.Set || r.Source != nil {
		t.Errorf("a name with no value is unset and has no source: %+v", r)
	}
	if r := by[fromEnv]; !r.Set || r.Source == nil || *r.Source != "env" {
		t.Errorf("a value from the environment says so: %+v", r)
	}
	if r := by["MCPX_SECRETS_TEST_RUNTIME_ONLY"]; !r.Set || r.Source == nil || *r.Source != "runtime" {
		t.Errorf("a runtime-only name is listed, from runtime: %+v", r)
	}
	if strings.Contains(rec.Body.String(), "env-value") || strings.Contains(rec.Body.String(), `"value"`) {
		t.Errorf("the listing carries values: %s", rec.Body)
	}
}

func TestACallToAWaitingServerIsRefusedBeforeAnythingRuns(t *testing.T) {
	g := newSecretsRig(t, envName)

	rec := g.do(t, http.MethodPost, "/v1/call", `{"server":"demo","tool":"x","args":{}}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a call to a waiting server is 409, got %d: %s", rec.Code, rec.Body)
	}
	body := decode[SecretRequiredBody](t, rec)
	if body.Code != "secret_required" || body.Server != "demo" || strings.Join(body.Secrets, ",") != envName {
		t.Fatalf("the refusal names the server and the secrets: %+v", body)
	}
	if !strings.HasPrefix(body.RequestID, "sr-") || body.Message == "" {
		t.Fatalf("the refusal carries a request id to wait on and a message: %+v", body)
	}
	if g.started() {
		t.Fatal("the server was started to fail: a refused call must not touch the child")
	}

	again := decode[SecretRequiredBody](t, g.do(t, http.MethodPost, "/v1/call", `{"server":"demo","tool":"x","args":{}}`))
	if again.RequestID != body.RequestID {
		t.Errorf("a second refusal waits on the same request: %s then %s", body.RequestID, again.RequestID)
	}
}

func TestPreflightReadsTheScriptAndRunsNothing(t *testing.T) {
	g := newSecretsRig(t, envName)
	script := `const a = await tools.demo.run({}); await tools.nowhere.gone(); await demo.more();`
	body, _ := json.Marshal(map[string]string{"source": script})

	rec := g.do(t, http.MethodPost, "/v1/preflight", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("preflight = %d: %s", rec.Code, rec.Body)
	}
	rep := decode[PreflightReport](t, rec)
	if strings.Join(rep.Namespaces, ",") != "demo,nowhere" {
		t.Errorf("namespaces are the ones the script reaches: %v", rep.Namespaces)
	}
	if strings.Join(rep.Unknown, ",") != "nowhere" {
		t.Errorf("unknown namespaces are the ones with no server: %v", rep.Unknown)
	}
	if rep.Ready || len(rep.Missing) != 1 || rep.Missing[0].Server != "demo" || strings.Join(rep.Missing[0].Secrets, ",") != envName {
		t.Fatalf("demo is not ready and says what it lacks: %+v", rep)
	}
	if len(rep.Requests) != 1 || !strings.HasPrefix(rep.Requests[0], "sr-") {
		t.Fatalf("one pending request covers the missing secret: %v", rep.Requests)
	}
	if g.started() {
		t.Fatal("preflight ran a tool: it must only read the script")
	}

	// Asked again, the same request answers: one request per server and set.
	again := decode[PreflightReport](t, g.do(t, http.MethodPost, "/v1/preflight", string(body)))
	if strings.Join(again.Requests, ",") != strings.Join(rep.Requests, ",") {
		t.Errorf("preflight reuses the pending request: %v then %v", rep.Requests, again.Requests)
	}
}

func TestPreflightOfAScriptThatReachesNothingIsReady(t *testing.T) {
	g := newSecretsRig(t, envName)
	rec := g.do(t, http.MethodPost, "/v1/preflight", `{"source":"console.log(1)"}`)
	rep := decode[PreflightReport](t, rec)
	if !rep.Ready || rep.Namespaces == nil || rep.Unknown == nil || rep.Missing == nil || rep.Requests == nil {
		t.Fatalf("nothing reached is ready, and every list is an array, not null: %+v", rep)
	}
	if !strings.Contains(rec.Body.String(), `"missing":[]`) || !strings.Contains(rec.Body.String(), `"requests":[]`) {
		t.Errorf("empty lists serialise as []: %s", rec.Body)
	}
}

func TestExecIsRefusedBeforeItsFirstStatement(t *testing.T) {
	g := newSecretsRig(t, envName)
	// The script's only statement would start the server if it ran. The
	// refusal has to come before the script does, which is what the rule is.
	body, _ := json.Marshal(map[string]any{"source": "await tools.demo.run({});"})
	rec := g.do(t, http.MethodPost, "/v1/exec", string(body))
	if rec.Code != http.StatusConflict {
		t.Fatalf("exec of a script that needs a waiting server is 409, got %d: %s", rec.Code, rec.Body)
	}
	if got := decode[SecretRequiredBody](t, rec); got.Code != "secret_required" || got.RequestID == "" {
		t.Fatalf("the refusal has the contract's shape: %+v", got)
	}
	if g.started() {
		t.Fatal("the script ran: the refusal must come before anything starts")
	}
}

func TestExecOfAFileIsCheckedTheSameWay(t *testing.T) {
	g := newSecretsRig(t, envName)
	file := filepath.Join(t.TempDir(), "job.ts")
	if err := os.WriteFile(file, []byte(`await tools.demo.run({});`), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := g.do(t, http.MethodPost, "/v1/exec", `{"file":"`+file+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a file that needs a waiting server is refused the same way, got %d: %s", rec.Code, rec.Body)
	}
	if g.started() {
		t.Fatal("the file ran")
	}
}

func TestSecretRequestLifecycle(t *testing.T) {
	g := newSecretsRig(t, envName)
	rep := decode[PreflightReport](t, g.do(t, http.MethodPost, "/v1/preflight", `{"source":"await tools.demo.x()"}`))
	id := rep.Requests[0]

	pending := decode[struct {
		Requests []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"requests"`
	}](t, g.do(t, http.MethodGet, "/v1/secret-requests", ""))
	if len(pending.Requests) != 1 || pending.Requests[0].ID != id || pending.Requests[0].Status != "pending" {
		t.Fatalf("the request is listed pending: %+v", pending)
	}

	// Setting the value resolves the request, on the very next read.
	if rec := g.do(t, http.MethodPut, "/v1/secrets/"+envName, `{"value":"tok"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d", rec.Code)
	}
	got := decode[struct {
		Status string `json:"status"`
	}](t, g.do(t, http.MethodGet, "/v1/secret-requests/"+id, ""))
	if got.Status != "resolved" {
		t.Fatalf("a request whose secrets are all set is resolved: %q", got.Status)
	}

	// Once the value is gone again, the next need is a new request: the old
	// one has ended, and only a pending request is reused.
	if rec := g.do(t, http.MethodDelete, "/v1/secrets/"+envName, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	fresh := decode[PreflightReport](t, g.do(t, http.MethodPost, "/v1/preflight", `{"source":"await tools.demo.x()"}`))
	if fresh.Requests[0] == id {
		t.Fatal("a resolved request is not reused for a later need")
	}

	dec := g.do(t, http.MethodDelete, "/v1/secret-requests/"+fresh.Requests[0], "")
	if dec.Code != http.StatusOK || !strings.Contains(dec.Body.String(), `"status":"declined"`) {
		t.Fatalf("declining answers the request as declined: %d %s", dec.Code, dec.Body)
	}
	if rec := g.do(t, http.MethodGet, "/v1/secret-requests/"+fresh.Requests[0], ""); !strings.Contains(rec.Body.String(), `"status":"declined"`) {
		t.Errorf("a declined request reads as declined: %s", rec.Body)
	}
}

func TestBlockedServerBlocksTheServersThatRequireIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"base": {Name: "base", Command: "/bin/sh", Args: []string{"-c", "touch " + marker + "; exit 1"},
			Mcpx: &config.Extras{RequiresSecrets: []string{envName}}},
		"top": {Name: "top", Command: "/bin/sh", Args: []string{"-c", "exit 1"},
			Mcpx: &config.Extras{RequiresServers: []string{"base"}}},
	}}
	srv, err := NewServer(Options{Config: cfg, Paths: Paths{State: dir, Cache: dir},
		Version: "test", Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.reg.Close)
	mux := http.NewServeMux()
	srv.routes(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}

	top := decode[serverRecord](t, do(http.MethodGet, "/v1/servers/top", ""))
	if top.Active || len(top.MissingSecrets) != 0 {
		t.Fatalf("top cannot run while base waits, though top itself lacks nothing: %+v", top)
	}
	rec := do(http.MethodPost, "/v1/call", `{"server":"top","tool":"x","args":{}}`)
	body := decode[SecretRequiredBody](t, rec)
	if rec.Code != http.StatusConflict || body.Server != "base" || strings.Join(body.Secrets, ",") != envName {
		t.Fatalf("the refusal names the server that is waiting, not the one asked for: %d %+v", rec.Code, body)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("base was started: a dependency that is waiting must not run")
	}
}

func TestWarmLeavesAWaitingServerAlone(t *testing.T) {
	g := newSecretsRig(t, envName)
	errs := g.srv.reg.Warm(context.Background(), true)
	if err, failed := errs["demo"]; failed {
		t.Errorf("a waiting server is not warmed, so it cannot fail warming: %v", err)
	}
	if g.started() {
		t.Fatal("warming started a server that is waiting for a secret")
	}
}

func TestTheRefusalIsAConflictAndNotAFault(t *testing.T) {
	err := SecretRequired{Server: "demo", Secrets: []string{envName}, RequestID: "sr-1", Message: "m"}
	if got := failureStatus(err); got != http.StatusConflict {
		t.Errorf("failureStatus = %d; want 409", got)
	}
	if body, ok := failureBody(err).(SecretRequiredBody); !ok || body.Code != "secret_required" || body.RequestID != "sr-1" {
		t.Errorf("failureBody = %#v; want the secret_required body", failureBody(err))
	}
}
