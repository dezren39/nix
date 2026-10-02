package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/api"
)

// probeValue is a value an operation will accept far enough to show the
// request reached its route: an id that does not exist is answered by the
// route, not by the mux.
func probeValue(p api.Param) string {
	switch p.Name {
	case "id":
		return "tsk-nothing"
	case "server":
		return "demo"
	case "tool":
		return "echo"
	case "name":
		return "summarise"
	case "session":
		return "s-nothing"
	case "kind":
		return "tools/call"
	case "answers", "ref", "argument", "args", "arguments":
		return "{}"
	}
	return "x"
}

// TestEveryGeneratedCommandReachesItsRoute is the CLI's counterpart to
// TestEveryDeclaredRouteAnswersOverTheSocket: every operation no
// hand-written command covers is run as a real command, and must reach its
// route rather than being refused by the argument parser or the mux.
func TestEveryGeneratedCommandReachesItsRoute(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")

	refused := []string{
		"unknown command", "404 page not found", "is required", "unexpected ",
		"flag provided but not defined", "has no \"", "needs one of",
		"is not a whole number", "not valid JSON",
	}
	ran := 0
	for _, op := range api.Ops() {
		if !op.GeneratedCommand() || op.Streams {
			continue
		}
		args := append([]string{}, op.CLIWords()...)
		for _, p := range op.Params {
			if p.DefaultCwd {
				continue
			}
			if p.In == api.InPath || p.Required {
				args = append(args, "--"+flagName(p.Name), probeValue(p))
			}
		}
		if op.Name == "task_result" || op.Name == "ask_poll" {
			args = append(args, "--wait-ms", "100")
		}
		out, _ := e.try(args...)
		ran++
		for _, bad := range refused {
			if strings.Contains(out, bad) {
				t.Errorf("mcpx %s: refused before reaching %s %s (%q):\n%s",
					strings.Join(args, " "), op.Method, op.Path, bad, out)
			}
		}
		if strings.Contains(out, "http 500") {
			t.Errorf("mcpx %s: the route failed:\n%s", strings.Join(args, " "), out)
		}
	}
	if ran == 0 {
		t.Fatal("no generated command was exercised")
	}
}

// flagName mirrors the CLI's spelling of a parameter as a flag.
func flagName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestArtifactsRoundTripThroughTheCLI is #40's `mcpx artifact get <id> -o
// path`, with bytes that are not text so that any re-encoding on the way
// shows up as a difference.
func TestArtifactsRoundTripThroughTheCLI(t *testing.T) {
	e := newEnv(t, oneServer)
	blob := make([]byte, 4096)
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(e.dir, "blob.bin")
	if err := os.WriteFile(src, blob, 0o644); err != nil {
		t.Fatal(err)
	}

	var put struct {
		ID   string `json:"id"`
		Size int    `json:"size"`
	}
	if err := json.Unmarshal([]byte(e.run("--json", "artifact", "put", "blob.bin", "@"+src)), &put); err != nil {
		t.Fatal(err)
	}
	if put.ID == "" || put.Size != len(blob) {
		t.Fatalf("put returned %+v, want an id and size %d", put, len(blob))
	}

	listed := e.run("artifact", "ls")
	if !strings.Contains(listed, put.ID) || !strings.Contains(listed, "blob.bin") {
		t.Fatalf("artifact ls does not show the artifact:\n%s", listed)
	}

	dst := filepath.Join(e.dir, "out.bin")
	e.run("artifact", "get", put.ID, "-o", dst)
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("artifact get -o wrote %d bytes that differ from the %d put", len(got), len(blob))
	}

	// stdin, which is how a pipeline hands one over.
	cmd := exec.Command(e.mcpx, "--json", "artifacts", "put", "note.txt", "-")
	cmd.Dir, cmd.Env, cmd.Stdin = e.dir, e.envVars, strings.NewReader("from stdin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("put from stdin: %v\n%s", err, out)
	}
	var note struct{ ID string }
	if err := json.Unmarshal(out, &note); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if text := e.run("artifact", "get", note.ID); strings.TrimSpace(text) != "from stdin" {
		t.Fatalf("artifact get printed %q", text)
	}

	e.run("artifact", "rm", put.ID)
	if listed := e.run("artifact", "list"); strings.Contains(listed, put.ID) {
		t.Fatalf("artifact rm left it listed:\n%s", listed)
	}
}

// TestMCPArtifactPutStoresTheContent: the generated MCP tool sent the body
// as a JSON object, so every artifact stored through it was the two bytes
// "{}". The content is the body, not a field of it.
func TestMCPArtifactPutStoresTheContent(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mcpx_artifact_put","arguments":{"name":"m.txt","content":"hello from mcp"}}}` + "\n"
	out := e.runStdin(in, "serve")
	if !strings.Contains(out, `\"size\": 14`) {
		t.Fatalf("mcpx_artifact_put did not store the 14 bytes it was given:\n%s", out)
	}
	var list struct {
		Artifacts []struct{ ID, Name string }
	}
	if err := json.Unmarshal([]byte(e.run("--json", "artifact", "list")), &list); err != nil {
		t.Fatal(err)
	}
	for _, a := range list.Artifacts {
		if a.Name == "m.txt" {
			if got := e.run("artifact", "get", a.ID); strings.TrimSpace(got) != "hello from mcp" {
				t.Fatalf("stored %q", got)
			}
			return
		}
	}
	t.Fatalf("m.txt was not stored: %+v", list)
}

func TestGeneratedCommandsRenderForPeopleAndForJSON(t *testing.T) {
	e := newEnv(t, oneServer)
	if out := e.run("task", "list"); !strings.Contains(out, "No tasks.") {
		t.Errorf("an empty listing should say so:\n%s", out)
	}
	// A bare noun lists, the way `mcpx elicit` does.
	if out := e.run("tasks"); !strings.Contains(out, "No tasks.") {
		t.Errorf("mcpx tasks should list:\n%s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(e.run("--json", "task", "ls")), &doc); err != nil {
		t.Fatalf("--json is not JSON: %v", err)
	}
	if _, ok := doc["tasks"]; !ok {
		t.Errorf("--json lost the document's shape: %v", doc)
	}
	// And after the command, the way people type it.
	if err := json.Unmarshal([]byte(e.run("task", "ls", "--json")), &doc); err != nil {
		t.Fatalf("a trailing --json is not honoured: %v", err)
	}

	health := e.run("health")
	if !strings.Contains(health, "status: ok") || !strings.Contains(health, "version:") {
		t.Errorf("health should render its fields:\n%s", health)
	}

	proto := e.run("--json", "protocol")
	if !strings.Contains(proto, "2026-07-28") {
		t.Errorf("protocol should list the revisions mcpx speaks:\n%s", proto)
	}
}

func TestResolveDefaultsToTheWorkingDirectory(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	var here, there map[string]any
	if err := json.Unmarshal([]byte(e.run("--json", "resolve")), &here); err != nil {
		t.Fatal(err)
	}
	cfg, _ := here["configPath"].(string)
	if !strings.HasSuffix(cfg, ".mcpx.json") {
		t.Fatalf("resolve with no directory should resolve the working one: %v", here)
	}
	if here["running"] != true {
		t.Errorf("the daemon serving this directory is running: %v", here)
	}
	// A relative directory is made absolute here, because the daemon's
	// working directory is not the caller's.
	sub := filepath.Join(e.dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(e.run("--json", "resolve", "sub")), &there); err != nil {
		t.Fatal(err)
	}
	if there["socket"] != here["socket"] {
		t.Errorf("a subdirectory inherits the same daemon: %v vs %v", there["socket"], here["socket"])
	}
}

func TestGeneratedCommandsRefuseBadArguments(t *testing.T) {
	e := newEnv(t, oneServer)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"task", "get"}, "id is required"},
		{[]string{"task", "frob"}, `has no "frob"`},
		{[]string{"task", "result", "tsk-x", "--wait-ms", "soon"}, "not a whole number"},
		{[]string{"task", "get", "tsk-x", "extra"}, `unexpected "extra"`},
		{[]string{"ask", "begin", "--kind", "nope", "--server", "demo"}, "not one of"},
		{[]string{"ask", "answers", "tsk-x", "--answers", "{bad"}, "not valid JSON"},
	}
	for _, c := range cases {
		out, err := e.try(c.args...)
		if err == nil {
			t.Errorf("mcpx %s succeeded; want a refusal", strings.Join(c.args, " "))
		}
		if !strings.Contains(out, c.want) {
			t.Errorf("mcpx %s: want %q in:\n%s", strings.Join(c.args, " "), c.want, out)
		}
	}
}

// TestEventsStreamAsNDJSON: the one streaming operation is reachable too,
// as one JSON document a line, so it pipes into jq.
func TestEventsStreamAsNDJSON(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// --since 0 so a child that has not finished subscribing when the
	// first restart fires gets that event late rather than never; see
	// TestAStreamingCommandWritesToTheFileItWasGiven below.
	cmd := exec.CommandContext(ctx, e.mcpx, "events", "--kinds", "server", "--since", "0")
	cmd.Dir, cmd.Env = e.dir, e.envVars
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	// Keep producing server events until one arrives: the subscription may
	// not be established by the time the first one fires.
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream ended without an event")
			}
			var ev struct{ Kind string }
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("not one JSON document a line: %q: %v", line, err)
			}
			if !strings.HasPrefix(ev.Kind, "server") {
				t.Fatalf("--kinds server let through %q", ev.Kind)
			}
			return
		case <-tick.C:
			_, _ = e.try("restart", "demo")
		case <-ctx.Done():
			t.Fatalf("no event within the deadline: %v", ctx.Err())
		}
	}
}

// TestDiagnoseOnAColdDaemonWaitsForTheSchemas: diagnose as the first command
// against a server slow to start compared the script with an empty catalog
// and reported nothing wrong. The delay makes the race deterministic.
func TestDiagnoseOnAColdDaemonWaitsForTheSchemas(t *testing.T) {
	e := newEnv(t, oneServer)
	// In the environment the server inherits, not in the config: newEnv
	// replaces FAKE in the config body with the binary's path, which renamed
	// FAKEMCP_START_DELAY and left the server starting at full speed.
	e.setenv("FAKEMCP_START_DELAY=1500ms")
	out, err := e.try("diagnose", "await demo.echo()")
	if err == nil || !strings.Contains(out, "message is required") {
		t.Fatalf("diagnose on a cold daemon should still find the missing argument:\n%s", out)
	}
}

// Every generated command prints `-o <file>` in its usage, so every generated
// command has to honour it -- including the streaming one, which returned
// before the flag was ever read and wrote to stdout instead. A flag that is
// advertised and ignored is exactly the defect the generated table exists to
// prevent.
func TestAStreamingCommandWritesToTheFileItWasGiven(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	out := filepath.Join(e.dir, "events.ndjson")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// --since 0 is not decoration; without it this test cannot pass reliably
	// and it failed in CI every single run while passing on a laptop.
	//
	// A subscription with no position is live only, and a freshly exec'd
	// child that has not finished subscribing when an event fires has lost
	// it for good. Locally the child wins that race in a few milliseconds;
	// under a loaded runner it loses it. When `restart` was stop-only it
	// lost deterministically: only the first restart had anything to stop,
	// so there was exactly one event to catch. Restart now publishes
	// server.stopped and server.started every time, but the position is
	// still what makes the test independent of the race.
	//
	// The daemon already answers that: /v1/events takes since= (and
	// Last-Event-ID), and present-but-zero means "everything still
	// retained". So the child asks for the history as well, a missed event
	// is a late one rather than a lost one, and `--since` and `--kinds` --
	// both advertised by the generated command -- are exercised rather than
	// avoided.
	cmd := exec.CommandContext(ctx, e.mcpx, "events", "--kinds", "server", "--since", "0", "-o", out)
	cmd.Dir, cmd.Env = e.dir, e.envVars
	// syncBuffer, not bytes.Buffer: this reads both while the child is still
	// running, which races os/exec's copier (see syncbuffer_test.go).
	var stdout, stderr syncBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Reaped here rather than in the defer, because whether the child is
	// still alive is the one thing this test could not see. An `events` that
	// exited immediately writes nothing, and an `events` that is healthy but
	// quiet also writes nothing -- for a day those produced the same failure
	// message, and the empty one was read as the wrong one of the two.
	//
	// The channel is closed rather than sent to: t.Fatalf runs this
	// function's defers on its way out, and a defer that re-reads a one-shot
	// channel the failure path already drained blocks forever. It did; the
	// package hit its 600s timeout instead of reporting in one second.
	exited := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(exited) }()
	defer func() {
		_ = cmd.Process.Kill()
		<-exited
	}()

	deadline := time.Now().Add(60 * time.Second)
	var got, provokeErr string
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			t.Fatalf("`mcpx events -o` exited while the stream was supposed to be open: %v\n"+
				"stderr: %s\nstdout: %s", waitErr, stderr.String(), stdout.String())
		default:
		}
		if _, err := e.try("restart", "demo"); err != nil {
			provokeErr = err.Error()
		}
		if b, rerr := os.ReadFile(out); rerr == nil && len(b) > 0 {
			got = string(b)
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if got == "" {
		t.Fatalf("-o is in this command's usage but nothing was written to %s\n"+
			"stderr: %s\nstdout: %s\nlast provoke error: %s",
			out, stderr.String(), stdout.String(), provokeErr)
	}
	if !strings.Contains(got, `"kind"`) {
		t.Errorf("the file should hold the event stream, got:\n%s", got)
	}
	if !strings.Contains(got, `"kind":"server.`) {
		t.Errorf("--kinds server should have let a server event through, got:\n%s", got)
	}
	if strings.Contains(stdout.String(), `"kind"`) {
		t.Errorf("-o means instead of printing, but stdout also got the stream:\n%s", stdout.String())
	}

	// Replay alone would not prove the stream is still live afterwards, and
	// -o has to keep appending, not write once. A restart publishes
	// server.stopped and server.started; a call against the warm instance it
	// leaves publishes call.* only, which --kinds must keep out.
	before := len(got)
	live := false
	liveDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(liveDeadline) {
		select {
		case <-exited:
			t.Fatalf("`mcpx events -o` exited after the replay, so the stream was not live: %v\n"+
				"stderr: %s", waitErr, stderr.String())
		default:
		}
		if _, err := e.try("call", "demo.echo", `{"message":"x"}`); err != nil {
			provokeErr = err.Error()
		}
		if _, err := e.try("restart", "demo"); err != nil {
			provokeErr = err.Error()
		}
		if b, rerr := os.ReadFile(out); rerr == nil && len(b) > before {
			got, live = string(b), true
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !live {
		t.Fatalf("the file stopped growing after the replay, so -o wrote the "+
			"history and not the live stream\nstderr: %s\nlast provoke error: %s\nfile:\n%s",
			stderr.String(), provokeErr, got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if !strings.Contains(line, `"kind":"server.`) {
			t.Errorf("--kinds server let through a line that is not a server event: %s", line)
		}
	}
}

// The property the test above depends on, asserted on its own and without a
// race to lose: an event that happened before the reader existed is still
// delivered when the reader names a position.
//
// Every event here is published before `mcpx events` is even spawned, so a
// live-only subscription can never see one. That is the deterministic form
// of what CI hit: the events a filtered stream would get had already fired
// by the time the child subscribed.
func TestAStreamPositionMakesAMissedEventALateOneNotALostOne(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"x"}`) // server.started
	e.run("restart", "demo")                      // server.stopped, server.started

	n := 0
	read := func(t *testing.T, window time.Duration, args ...string) string {
		t.Helper()
		n++
		out := filepath.Join(e.dir, fmt.Sprintf("pos-%d.ndjson", n))
		ctx, cancel := context.WithTimeout(context.Background(), window+10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, e.mcpx, append(args, "-o", out)...)
		cmd.Dir, cmd.Env = e.dir, e.envVars
		var stderr syncBuffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		deadline := time.Now().Add(window)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
				return string(b)
			}
			time.Sleep(50 * time.Millisecond)
		}
		if stderr.Len() > 0 {
			t.Logf("stderr: %s", stderr.String())
		}
		return ""
	}

	t.Run("with a position the history arrives", func(t *testing.T) {
		got := read(t, 20*time.Second, "events", "--kinds", "server", "--since", "0")
		if !strings.Contains(got, `"kind":"server.`) {
			t.Fatalf("--since 0 should replay the retained server events, got:\n%s", got)
		}
	})
	t.Run("without one it does not", func(t *testing.T) {
		// Nothing publishes during this window, so an empty file is the
		// answer and not a slow one. This is the half that shows --since is
		// carrying the test above rather than decorating it.
		if got := read(t, 3*time.Second, "events", "--kinds", "server"); got != "" {
			t.Fatalf("a positionless subscription is live only; it should not have replayed:\n%s", got)
		}
	})
}

// restart is a restart: every time it stops the running instance and brings
// a fresh one up, publishing server.stopped then server.started. It used to
// stop only, so the second restart reported "stopped 0" and published nothing.
func TestRestartRestartsEveryTime(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("ls")
	for i := 0; i < 2; i++ {
		out := e.run("restart", "demo")
		if !strings.Contains(out, "restarted 1 instance(s) for demo") {
			t.Fatalf("restart %d: %s", i+1, out)
		}
	}
	if st := e.run("status"); !strings.Contains(st, "demo#") {
		t.Fatalf("no live instance after restart:\n%s", st)
	}

	out := filepath.Join(e.dir, "events.ndjson")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.mcpx, "events", "--kinds", "server", "--since", "0", "-o", out)
	cmd.Dir, cmd.Env = e.dir, e.envVars
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	// After the last restart's stop, a start must follow.
	startedAfterStop := func(s string) bool {
		i := strings.LastIndex(s, `"kind":"server.stopped"`)
		return i >= 0 && strings.Contains(s[i:], `"kind":"server.started"`)
	}
	var got string
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		b, _ := os.ReadFile(out)
		if got = string(b); strings.Count(got, `"kind":"server.stopped"`) >= 2 && startedAfterStop(got) {
			return
		}
	}
	t.Fatalf("want two server.stopped each followed by server.started, got:\n%s", got)
}

// A replacement that does not come up fails the restart now, with the
// server's own stderr and a non-zero exit, instead of on the next call.
func TestRestartReportsAServerThatDoesNotComeBack(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("FAKEMCP_FAIL_START=1")
	out, err := e.try("restart", "demo")
	if err == nil {
		t.Fatalf("restart of a server that cannot start exited 0:\n%s", out)
	}
	if !strings.Contains(out, "refusing to start") {
		t.Fatalf("the failure should carry the server's stderr:\n%s", out)
	}
	// --lazy is the old stop-only behaviour and starts nothing to fail.
	if out := e.run("restart", "--lazy", "demo"); !strings.Contains(out, "stopped 0 instance(s) for demo") {
		t.Fatalf("restart --lazy: %s", out)
	}
}
