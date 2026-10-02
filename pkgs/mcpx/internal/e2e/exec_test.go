package e2e_test

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// An artifact is the answer to "a script produced a file". These tests drive
// the real binary against a real daemon, because the whole feature is about
// what crosses a process boundary and an in-process test cannot see that.

func TestExecWritesArtifactsIntoADirectory(t *testing.T) {
	e := newEnv(t, oneServer)
	out := filepath.Join(e.dir, "collected")
	e.run("exec", "--artifacts-dir", out,
		`await artifact("shot.png", new Uint8Array([137,80,78,71])); console.log("ok")`)

	body, err := os.ReadFile(filepath.Join(out, "shot.png"))
	if err != nil {
		t.Fatalf("the named directory should hold the file: %v", err)
	}
	if len(body) != 4 || body[0] != 137 {
		t.Errorf("body = %v", body)
	}
}

// A screenshot tool answers with a caption and then the image, as
// chrome-devtools' take_screenshot does. artifact("shot.png", result) must
// store the image: the caption is a text block too, and taking the first
// block with a body stored the caption under a .png name.
func TestArtifactOfAToolResultPrefersItsMediaOverItsText(t *testing.T) {
	e := newEnv(t, oneServer)
	out := filepath.Join(e.dir, "collected")
	// fakemcp's structured tool returns a text block, then a PNG image.
	e.run("exec", "--artifacts-dir", out,
		`const a = await artifact("shot.png", await demo.structured({})); console.log(a.mime)`)
	body, err := os.ReadFile(filepath.Join(out, "shot.png"))
	if err != nil {
		t.Fatal(err)
	}
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgo=")
	if string(body) != string(png) {
		t.Errorf("stored %q, want the image block's bytes %q", body, png)
	}
}

// Two files of one name are two files. Keeping one of them silently is data
// loss, and overwriting is the easy mistake to make here.
func TestArtifactNameCollisionsAreSuffixedNotOverwritten(t *testing.T) {
	e := newEnv(t, oneServer)
	out := filepath.Join(e.dir, "collected")
	e.run("exec", "--artifacts-dir", out,
		`await artifact("a.txt", "first"); await artifact("a.txt", "second"); console.log("ok")`)

	first, err := os.ReadFile(filepath.Join(out, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(out, "a-1.txt"))
	if err != nil {
		t.Fatalf("the second file should be beside the first: %v", err)
	}
	if string(first) == string(second) {
		t.Errorf("both files hold %q", first)
	}
}

// A script is not trusted with a path.
func TestArtifactNamesCannotEscapeTheOutputDirectory(t *testing.T) {
	e := newEnv(t, oneServer)
	out := filepath.Join(e.dir, "collected")
	e.run("exec", "--artifacts-dir", out,
		`await artifact("../../escaped.txt", "nope"); console.log("ok")`)

	if _, err := os.Stat(filepath.Join(e.dir, "escaped.txt")); err == nil {
		t.Fatal("a name with .. wrote outside the directory it was given")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "escaped.txt" {
		t.Errorf("want one sanitised file, got %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// The local case: the daemon shares this filesystem, so handing a file back
// should cost an inode rather than a copy.
func TestALocalFileIsLinkedIntoTheStoreNotCopied(t *testing.T) {
	e := newEnv(t, oneServer)
	src := filepath.Join(e.dir, "source.bin")
	if err := os.WriteFile(src, []byte("linked bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := e.run("--json", "exec",
		fmt.Sprintf(`const a = await artifact("source.bin", { path: %q }); console.log(JSON.stringify(a))`, src))
	var env struct {
		Result struct {
			ID   string `json:"id"`
			Size int64  `json:"size"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &env); err != nil {
		t.Fatal(err)
	}
	if env.Result.Size != 12 {
		t.Fatalf("size = %d (%s)", env.Result.Size, out)
	}
	var st syscall.Stat_t
	if err := syscall.Stat(src, &st); err != nil {
		t.Fatal(err)
	}
	if st.Nlink < 2 {
		t.Error("the store should have linked the file, not read and rewritten it")
	}
}

func TestExecOverV1ReturnsAStructuredResultWithArtifacts(t *testing.T) {
	e := newEnv(t, oneServer)
	// `status` reports a socket only once a daemon is up, and it does not
	// start one itself.
	e.run("ls")
	client := e.socketClient(t)
	body := postExec(t, client, map[string]any{
		"source": `log.info("note"); emit({ n: 1 });
			await artifact("remote.png", new Uint8Array([1,2,3]));
			console.log(JSON.stringify({ ok: true }))`,
		"options": map[string]any{"capabilities": []string{"artifacts"}},
	})
	var res struct {
		RunID     string            `json:"runId"`
		Result    map[string]any    `json:"result"`
		Emits     []json.RawMessage `json:"emits"`
		Logs      []map[string]any  `json:"logs"`
		Artifacts []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Mime string `json:"mime"`
			Size int64  `json:"size"`
			URI  string `json:"uri"`
			Data string `json:"data"`
		} `json:"artifacts"`
		ExitCode int `json:"exitCode"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if res.ExitCode != 0 || res.RunID == "" {
		t.Fatalf("unexpected result: %s", body)
	}
	if len(res.Emits) != 1 || len(res.Logs) != 1 {
		t.Errorf("emits=%d logs=%d: %s", len(res.Emits), len(res.Logs), body)
	}
	if res.Result["ok"] != true {
		t.Errorf("stdout JSON should arrive parsed: %s", body)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("artifacts = %d: %s", len(res.Artifacts), body)
	}
	a := res.Artifacts[0]
	if a.Name != "remote.png" || a.Mime != "image/png" || a.Size != 3 {
		t.Errorf("metadata wrong: %+v", a)
	}
	// Reference delivery by default: the metadata travels, the bytes do not.
	if a.Data != "" {
		t.Error("reference delivery should not carry the body")
	}
	if a.URI != "mcpx://artifacts/"+a.ID {
		t.Errorf("uri = %q", a.URI)
	}

	// (b) fetchable by id, with Range.
	resp, err := client.Get("http://mcpx/v1/artifacts/" + a.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "remote.png") {
		t.Errorf("content disposition = %q", cd)
	}
	got := make([]byte, 8)
	n, _ := resp.Body.Read(got)
	if n != 3 || got[0] != 1 {
		t.Errorf("body = %v", got[:n])
	}

	req, _ := http.NewRequest(http.MethodGet, "http://mcpx/v1/artifacts/"+a.ID, nil)
	req.Header.Set("Range", "bytes=1-2")
	rr, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Body.Close()
	if rr.StatusCode != http.StatusPartialContent {
		t.Errorf("a range request should be answered with 206, got %d", rr.StatusCode)
	}

	// Filtering by run is how a caller collects one execution's output.
	listed := getJSON(t, client, "/v1/artifacts?run="+res.RunID)
	blob, _ := json.Marshal(listed)
	if !strings.Contains(string(blob), a.ID) {
		t.Errorf("the run's artifact should be listed: %s", blob)
	}

	del, err := client.Do(mustReq(t, http.MethodDelete, "http://mcpx/v1/artifacts/"+a.ID))
	if err != nil {
		t.Fatal(err)
	}
	del.Body.Close()
	if del.StatusCode != http.StatusOK {
		t.Errorf("delete = %d", del.StatusCode)
	}
	after, err := client.Get("http://mcpx/v1/artifacts/" + a.ID)
	if err != nil {
		t.Fatal(err)
	}
	after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Errorf("a deleted artifact should be gone, got %d", after.StatusCode)
	}
}

// The ordering contract: bodies come after the end frame, so a consumer can
// read the answer and disconnect before paying for them.
func TestExecStreamPutsBodiesAfterTheEndFrame(t *testing.T) {
	e := newEnv(t, oneServer)
	// `status` reports a socket only once a daemon is up, and it does not
	// start one itself.
	e.run("ls")
	client := e.socketClient(t)
	resp := postStream(t, client, "http://mcpx/v1/exec", map[string]any{
		"source": `emit({ step: 1 }); await artifact("s.txt", "streamed body");
			console.log(JSON.stringify({ done: true }))`,
		"options": map[string]any{
			"output":       "stream",
			"capabilities": []string{"artifacts"},
			"artifacts":    map[string]any{"delivery": "stream"},
		},
	})
	defer resp.Body.Close()

	var kinds []string
	var reassembled strings.Builder
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var f struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if json.Unmarshal([]byte(line), &f) != nil {
			t.Fatalf("not a frame: %s", line)
		}
		kinds = append(kinds, f.Type)
		if f.Type == "artifact.chunk" {
			raw, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				t.Fatal(err)
			}
			reassembled.Write(raw)
		}
	}
	if kinds[0] != "start" {
		t.Errorf("first frame = %q", kinds[0])
	}
	endAt, chunkAt := indexOf(kinds, "end"), indexOf(kinds, "artifact.chunk")
	if endAt < 0 || chunkAt < 0 {
		t.Fatalf("frames: %v", kinds)
	}
	if chunkAt < endAt {
		t.Errorf("a body arrived before the end frame, so a consumer could not "+
			"have cancelled first: %v", kinds)
	}
	if indexOf(kinds, "artifact") > endAt {
		t.Errorf("artifact metadata belongs before the end: %v", kinds)
	}
	if reassembled.String() != "streamed body" {
		t.Errorf("reassembled = %q", reassembled.String())
	}
}

// A consumer that has what it wanted may stop reading. Nothing should be
// left running on the daemon, and nothing half-written on disk.
func TestAStreamConsumerCanCancelAfterTheResult(t *testing.T) {
	e := newEnv(t, oneServer)
	// `status` reports a socket only once a daemon is up, and it does not
	// start one itself.
	e.run("ls")
	client := e.socketClient(t)
	resp := postStream(t, client, "http://mcpx/v1/exec", map[string]any{
		"source": `await artifact("big.bin", "x".repeat(200000));
			console.log(JSON.stringify({ done: true }))`,
		"options": map[string]any{
			"output":       "stream",
			"capabilities": []string{"artifacts"},
			"artifacts":    map[string]any{"delivery": "stream"},
		},
	})
	sc := bufio.NewScanner(resp.Body)
	// This failed once inside nix build on main (run 36821463044) and has not
	// reproduced in 60 local runs. The test could only say it never saw the
	// end frame, so it now reports every frame it did see and why the scan
	// stopped. The larger line limit is a precaution, not the cause: forcing
	// the default 64 KB back still passes.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	saw := ""
	var kinds []string
	for sc.Scan() {
		var f struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			kinds = append(kinds, fmt.Sprintf("<unparsed %d bytes>", len(sc.Bytes())))
			continue
		}
		kinds = append(kinds, f.Type)
		if f.Type == "end" {
			saw = f.Type
			break
		}
	}
	scanErr := sc.Err()
	// Closed mid-stream, with bodies still to come.
	resp.Body.Close()
	if saw != "end" {
		t.Fatalf("the run should have reached its end frame; status %d, frames %v, scan error %v",
			resp.StatusCode, kinds, scanErr)
	}

	// The daemon is still healthy and still serving, which is the thing a
	// cancelled stream most plausibly breaks.
	if out := e.run("--json", "status"); !strings.Contains(out, "socket") {
		t.Errorf("the daemon did not survive a cancelled stream: %s", out)
	}
}

func TestExecOnTheDaemonRunsWhereTheDaemonIs(t *testing.T) {
	e := newEnv(t, oneServer)
	// --remote sends the script text; the file it writes lands on the
	// daemon's filesystem, which here is the same one, so the assertion is
	// about the route being taken rather than about the bytes moving.
	out := e.run("exec", "--remote", "--output", "structured",
		`await artifact("from-daemon.txt", "hello"); console.log("ran")`)
	var res struct {
		Artifacts []struct {
			Name string `json:"name"`
		} `json:"artifacts"`
		Stdout string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, out)), &res); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(res.Stdout, "ran") {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if len(res.Artifacts) != 1 || res.Artifacts[0].Name != "from-daemon.txt" {
		t.Errorf("artifacts = %+v", res.Artifacts)
	}
}

// (a) of the three surfaces: an MCP client gets a resource_link and can read
// the artifact as a resource. The megabyte never enters the model's context.
func TestMCPExecReturnsAResourceLinkAndTheResourceIsReadable(t *testing.T) {
	e := newEnv(t, oneServer)
	body := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_exec","arguments":{"source":"await artifact(\"mcp.png\", new Uint8Array([9,9,9])); console.log(\"printed\")"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"resources/templates/list","params":{}}`,
	}, "\n") + "\n"
	out := e.runStdin(body, "serve")

	var link struct {
		URI  string `json:"uri"`
		Name string `json:"name"`
		Mime string `json:"mimeType"`
	}
	var sawText bool
	for _, line := range strings.Split(out, "\n") {
		var frame struct {
			ID     int `json:"id"`
			Result struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
					URI  string `json:"uri"`
					Name string `json:"name"`
					Mime string `json:"mimeType"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &frame) != nil || frame.ID != 2 {
			continue
		}
		for _, c := range frame.Result.Content {
			switch c.Type {
			case "text":
				sawText = sawText || strings.Contains(c.Text, "printed")
			case "resource_link":
				link.URI, link.Name, link.Mime = c.URI, c.Name, c.Mime
			}
		}
	}
	if !sawText {
		t.Errorf("the script's own output should still come back:\n%s", out)
	}
	if link.URI == "" {
		t.Fatalf("no resource_link in the result:\n%s", out)
	}
	if link.Name != "mcp.png" || link.Mime != "image/png" {
		t.Errorf("link = %+v", link)
	}
	if !strings.Contains(out, "mcpx://artifacts/{id}") {
		t.Errorf("the artifact resource template should be advertised:\n%s", out)
	}

	read := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"` + link.URI + `"}}`,
	}, "\n") + "\n"
	got := e.runStdin(read, "serve")
	want := base64.StdEncoding.EncodeToString([]byte{9, 9, 9})
	if !strings.Contains(got, want) {
		t.Errorf("reading the resource should return the bytes (base64 %q):\n%s", want, got)
	}
}

func TestArtifactsAreRefusedOverTheSizeLimit(t *testing.T) {
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_ARTIFACTS_MAX_BYTES=16")
	out, err := e.try("exec", `await artifact("big.bin", "x".repeat(1000)); console.log("ok")`)
	if err == nil {
		t.Fatalf("over the limit should fail the script:\n%s", out)
	}
	if !strings.Contains(out, "limit") && !strings.Contains(out, "maxBytes") {
		t.Errorf("the error should say what was exceeded:\n%s", out)
	}
}

// ---- helpers ----

func indexOf(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}

func mustReq(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// postExec runs one /v1/exec request and returns the whole answer.
func postExec(t *testing.T, client *http.Client, body any) []byte {
	t.Helper()
	resp := postStream(t, client, "http://mcpx/v1/exec", body)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("http %d: %s", resp.StatusCode, b)
	}
	return b
}

// postStream issues a POST and hands back the live body.
//
// The socket client carries a short timeout, which is right for a request and
// fatal for a stream: the response lasts as long as the script does.
func postStream(t *testing.T, client *http.Client, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	long := *client
	long.Timeout = 90 * time.Second
	resp, err := long.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// Three renderings of one run. The failure this guards against is a bare
// JSON line landing in the middle of a document a caller is parsing.
func TestTheThreeOutputShapesDoNotContaminateEachOther(t *testing.T) {
	e := newEnv(t, oneServer)
	script := `emit({ a: 1 }); console.log(JSON.stringify({ ok: 1 }))`

	structured := e.run("exec", "--output", "structured", script)
	var doc map[string]any
	if err := json.Unmarshal([]byte(jsonOf(t, structured)), &doc); err != nil {
		t.Fatalf("structured output should be one document: %v\n%s", err, structured)
	}
	if strings.Count(structured, "\n{\"a\"") > 0 {
		t.Errorf("the emit was also printed raw:\n%s", structured)
	}
	emits, _ := doc["emits"].([]any)
	if len(emits) != 1 {
		t.Errorf("emits = %v", doc["emits"])
	}

	stream := e.run("exec", "--output", "stream", script)
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		var f struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			t.Fatalf("every line of a stream must be a frame, got %q", line)
		}
		kinds = append(kinds, f.Type)
	}
	if indexOf(kinds, "emit") < 0 || indexOf(kinds, "stdout") < 0 || kinds[len(kinds)-1] != "end" {
		t.Errorf("frames = %v", kinds)
	}

	// Text is unchanged: the emit on its own line, then what was printed.
	text := e.run("exec", script)
	if !strings.Contains(text, `{"a":1}`) || !strings.Contains(text, `{"ok":1}`) {
		t.Errorf("text output changed shape:\n%s", text)
	}
}
