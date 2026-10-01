package execsvc_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/execsvc"
	"github.com/dezren39/mcpx/internal/runner"
)

func store(t *testing.T) *artifacts.Store {
	t.Helper()
	s, err := artifacts.Open(artifacts.Options{Dir: t.TempDir(), TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func service(t *testing.T, st *artifacts.Store) *execsvc.Service {
	t.Helper()
	lim := execsvc.DefaultLimits()
	// No runtime is started in these tests; a short bound means a mistake
	// fails rather than hangs.
	lim.Timeout = 5 * time.Second
	return &execsvc.Service{Store: st, Limits: lim, Local: true}
}

// The runtime is a real one. Skipping rather than failing keeps the suite
// honest on a machine that has none: these tests are about the service, and
// a missing deno is not a bug in it.
func needRuntime(t *testing.T) {
	t.Helper()
	if _, err := runner.Resolve("auto", "", runner.Setup{}); err != nil {
		t.Skip("no JavaScript runtime:", err)
	}
}

func run(t *testing.T, svc *execsvc.Service, source string, opts execsvc.Options) (*execsvc.Result, []execsvc.Frame) {
	t.Helper()
	needRuntime(t)
	var frames []execsvc.Frame
	res, err := svc.RunWith(context.Background(), runner.Options{
		Source:       source,
		ClientSource: minimalClient,
		Permissions:  "all",
	}, opts, func(f execsvc.Frame) error {
		frames = append(frames, f)
		return nil
	})
	if err != nil {
		t.Fatalf("run: %v (%+v)", err, res)
	}
	return res, frames
}

// minimalClient is enough of the generated module for the launcher's import
// to resolve. The real one is generated from live servers, which these tests
// deliberately do not need.
const minimalClient = `
export const log = Object.assign(() => {}, {
  debug(){}, info(m: string){ write("log", { level: "info", msg: m }); },
  warn(){}, error(){}, with(){ return log; }, enabled(){ return true; },
});
function write(kind: string, payload: Record<string, unknown>) {
  const line = "\u001emcpx\u001e" + JSON.stringify({ kind, ts: new Date().toISOString(), ...payload });
  const enc = new TextEncoder().encode(line + "\n");
  (globalThis as any).Deno?.stderr?.writeSync?.(enc) ??
    (globalThis as any).process?.stderr?.write?.(line + "\n");
}
export function emit(value: unknown) { write("result", { value }); }
export const emitResult = emit;
export const tools = {};
export function installGlobals() {
  const g = globalThis as any;
  g.log ??= log; g.emit ??= emit;
}
export function captureConsole() {}
export function releaseConsole() {}
export function call() {}
export function readResource() {}
export function artifact() {}
export class ToolError extends Error {}
export default tools;
`

func TestFramesArriveInOrderAndEndLast(t *testing.T) {
	svc := service(t, store(t))
	_, frames := run(t, svc, `
import { log, emit, installGlobals } from "./mcpx-client.ts";
installGlobals();
log.info("working");
emit({ step: 1 });
console.log(JSON.stringify({ done: true }));
`, execsvc.Options{})

	var kinds []string
	for _, f := range frames {
		kinds = append(kinds, f.Type)
	}
	joined := strings.Join(kinds, ",")
	if !strings.HasPrefix(joined, "start,") {
		t.Errorf("a run must announce itself first: %s", joined)
	}
	if kinds[len(kinds)-1] != execsvc.FrameEnd {
		t.Errorf("end must be last when nothing is streamed: %s", joined)
	}
	for _, want := range []string{execsvc.FrameLog, execsvc.FrameEmit, execsvc.FrameStdout, execsvc.FrameResult} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s frame: %s", want, joined)
		}
	}
	if idx(kinds, execsvc.FrameResult) > idx(kinds, execsvc.FrameEnd) {
		t.Errorf("the result must come before the end: %s", joined)
	}
}

func idx(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return len(s)
}

func TestStructuredResultCarriesEverything(t *testing.T) {
	svc := service(t, store(t))
	res, _ := run(t, svc, `
import { log, emit, installGlobals } from "./mcpx-client.ts";
installGlobals();
log.info("a note");
emit({ n: 1 });
emit({ n: 2 });
console.log(JSON.stringify({ total: 2 }));
`, execsvc.Options{})

	if len(res.Emits) != 2 {
		t.Errorf("emits = %d", len(res.Emits))
	}
	if len(res.Logs) != 1 || res.Logs[0].Message != "a note" {
		t.Errorf("logs = %+v", res.Logs)
	}
	if !strings.Contains(res.Stdout, "total") {
		t.Errorf("stdout = %q", res.Stdout)
	}
	m, ok := res.Result.(map[string]any)
	if !ok || m["total"] != float64(2) {
		t.Errorf("JSON on stdout should be offered parsed, got %#v", res.Result)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, error = %q", res.ExitCode, res.Error)
	}
	if res.RunID == "" {
		t.Error("a run should be identifiable")
	}
}

// A consumer that leaves must stop the script. Otherwise a cancelled stream
// leaves work running on the daemon that nobody will ever read.
func TestASinkThatFailsStopsTheRun(t *testing.T) {
	needRuntime(t)
	svc := service(t, store(t))
	// The failure is deferred past the start frame so that what is being
	// tested is a consumer leaving mid-run, not one that was never there.
	frames := 0
	started := time.Now()
	_, err := svc.RunWith(context.Background(), runner.Options{
		Source: `
import { log, installGlobals } from "./mcpx-client.ts";
installGlobals();
log.info("running");
await new Promise((r) => setTimeout(r, 60000));
`,
		ClientSource: minimalClient,
		Permissions:  "all",
	}, execsvc.Options{}, func(f execsvc.Frame) error {
		frames++
		if f.Type == execsvc.FrameLog {
			return errClosed
		}
		return nil
	})
	if err == nil {
		t.Fatal("the sink's error should surface")
	}
	if frames < 2 {
		t.Fatalf("the run should have got past start, saw %d frames", frames)
	}
	// The script asked to sleep a minute. Anything near that means the
	// cancelled context did not reach the process group.
	if d := time.Since(started); d > 20*time.Second {
		t.Errorf("the script outlived its consumer by %s", d)
	}
}

var errClosed = &closedErr{}

type closedErr struct{}

func (*closedErr) Error() string { return "consumer went away" }

// An artifact in the index for this run is found even when the script never
// announced it, and delivered under the caller's terms.
func TestArtifactsAreReconciledAndDeliveredIntoADirectory(t *testing.T) {
	needRuntime(t)
	st := store(t)
	svc := service(t, st)
	runID := execsvc.NewRunID()
	if _, err := st.Put(strings.NewReader("BODY"), artifacts.PutOptions{
		Name: "shot.png", Run: runID,
	}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "collected")

	var frames []execsvc.Frame
	res, err := svc.RunWith(context.Background(), runner.Options{
		Source:       "console.log(\"ok\");",
		ClientSource: minimalClient,
		Permissions:  "all",
		Env:          map[string]string{"MCPX_RUN": runID},
	}, execsvc.Options{
		Capabilities: []string{execsvc.CapabilityArtifacts},
		Artifacts:    &execsvc.ArtifactOptions{Dir: out},
	}, func(f execsvc.Frame) error { frames = append(frames, f); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("the index should have been consulted: %+v", res.Artifacts)
	}
	got := res.Artifacts[0]
	if got.Path == "" {
		t.Fatal("a named directory should have received the file")
	}
	body, rerr := os.ReadFile(got.Path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(body) != "BODY" {
		t.Errorf("body = %q", body)
	}
	if filepath.Base(got.Path) != "shot.png" {
		t.Errorf("name = %s", filepath.Base(got.Path))
	}
	// Placed, not encoded: a caller that can read the directory has no use
	// for a base64 copy of what is already on its disk.
	if got.Data != "" {
		t.Error("a placed artifact should not also be inlined")
	}
}

func TestInlineDeliveryBoundsWhatItEncodes(t *testing.T) {
	needRuntime(t)
	st := store(t)
	svc := service(t, st)
	runID := execsvc.NewRunID()
	if _, err := st.Put(strings.NewReader("0123456789"), artifacts.PutOptions{
		Name: "big.bin", Run: runID,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.RunWith(context.Background(), runner.Options{
		Source:       "console.log(\"ok\");",
		ClientSource: minimalClient,
		Permissions:  "all",
		Env:          map[string]string{"MCPX_RUN": runID},
	}, execsvc.Options{
		Capabilities: []string{execsvc.CapabilityArtifacts},
		Artifacts:    &execsvc.ArtifactOptions{Delivery: execsvc.DeliveryInline, MaxBytes: 4},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Artifacts[0].Data != "" {
		t.Error("an artifact over the caller's ceiling should stay a reference")
	}
	if res.Artifacts[0].URI == "" {
		t.Error("a reference still has to say where to find it")
	}
}

func TestStreamedBodiesComeAfterTheEndFrame(t *testing.T) {
	needRuntime(t)
	st := store(t)
	svc := service(t, st)
	runID := execsvc.NewRunID()
	if _, err := st.Put(strings.NewReader("streamed body"), artifacts.PutOptions{
		Name: "s.txt", Run: runID,
	}); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var data strings.Builder
	_, err := svc.RunWith(context.Background(), runner.Options{
		Source:       "console.log(\"ok\");",
		ClientSource: minimalClient,
		Permissions:  "all",
		Env:          map[string]string{"MCPX_RUN": runID},
	}, execsvc.Options{
		Capabilities: []string{execsvc.CapabilityArtifacts},
		Artifacts:    &execsvc.ArtifactOptions{Delivery: execsvc.DeliveryStream},
	}, func(f execsvc.Frame) error {
		kinds = append(kinds, f.Type)
		if f.Type == execsvc.FrameArtifactData {
			raw, derr := base64.StdEncoding.DecodeString(f.Data)
			if derr != nil {
				return derr
			}
			data.Write(raw)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if data.String() != "streamed body" {
		t.Errorf("reassembled = %q", data.String())
	}
	end := idx(kinds, execsvc.FrameEnd)
	chunk := idx(kinds, execsvc.FrameArtifactData)
	if chunk < end {
		t.Errorf("bodies must come after end so a consumer can cancel first: %v", kinds)
	}
	if kinds[len(kinds)-1] != execsvc.FrameArtifactEnd {
		t.Errorf("each body should be closed off: %v", kinds)
	}
}

// The point of interception: a megabyte of base64 in a result becomes a
// reference, and only when the caller said it can receive one.
func TestInlineImagesBecomeArtifactReferences(t *testing.T) {
	needRuntime(t)
	st := store(t)
	svc := service(t, st)
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG-bytes"))
	source := `
import { emit, installGlobals } from "./mcpx-client.ts";
installGlobals();
console.log(JSON.stringify({ content: [{ type: "image", data: "` + png + `", mimeType: "image/png" }] }));
`
	res, _ := run(t, svc, source, execsvc.Options{
		Capabilities: []string{execsvc.CapabilityArtifacts},
	})
	blob, _ := json.Marshal(res.Result)
	if strings.Contains(string(blob), png) {
		t.Errorf("the base64 survived into the result: %s", blob)
	}
	if !strings.Contains(string(blob), "resource_link") {
		t.Errorf("want a resource_link, got %s", blob)
	}
	if len(res.Artifacts) != 1 {
		t.Fatalf("the bytes should have been stored: %+v", res.Artifacts)
	}
	if res.Artifacts[0].Mime != "image/png" {
		t.Errorf("mime = %q", res.Artifacts[0].Mime)
	}
	body, _, err := st.Bytes(res.Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "\x89PNG-bytes" {
		t.Errorf("stored body = %q", body)
	}
}

func TestInlineImagesAreLeftAloneWhenTheCallerCannotReceiveArtifacts(t *testing.T) {
	needRuntime(t)
	svc := service(t, store(t))
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG"))
	res, _ := run(t, svc, `
import { installGlobals } from "./mcpx-client.ts";
installGlobals();
console.log(JSON.stringify({ content: [{ type: "image", data: "`+png+`", mimeType: "image/png" }] }));
`, execsvc.Options{})
	blob, _ := json.Marshal(res.Result)
	if !strings.Contains(string(blob), png) {
		t.Errorf("a caller that declared nothing should get what the tool sent: %s", blob)
	}
}

func TestWantsArtifactsReadsBothWaysOfAskingForThem(t *testing.T) {
	if (execsvc.Options{}).WantsArtifacts() {
		t.Error("silence is not a declaration")
	}
	if !(execsvc.Options{Capabilities: []string{execsvc.CapabilityArtifacts}}).WantsArtifacts() {
		t.Error("the declared capability should count")
	}
	if !(execsvc.Options{Artifacts: &execsvc.ArtifactOptions{Dir: "/tmp"}}).WantsArtifacts() {
		t.Error("naming a directory is declaring the capability")
	}
}

func TestAFailedScriptExplainsItself(t *testing.T) {
	// A script that runs and exits non-zero is not an error to the runner,
	// so nothing filled in Error and the script's stderr was discarded: the
	// caller got exitCode 1 and nothing else. Over /v1 that is the whole
	// diagnostic an agent would ever see.
	needRuntime(t)
	svc := service(t, store(t))
	for _, src := range []string{
		`throw new Error("boom");`,
		`return 1;`, // illegal at the top level of a module
		`nonexistent_fn();`,
	} {
		res, _ := svc.RunWith(context.Background(), runner.Options{
			Source: src, ClientSource: minimalClient, Permissions: "all",
		}, execsvc.Options{Output: execsvc.OutputStructured}, func(execsvc.Frame) error { return nil })
		if res == nil || res.ExitCode == 0 {
			t.Errorf("%s: expected a failure", src)
			continue
		}
		if strings.TrimSpace(res.Error) == "" {
			t.Errorf("%s: exit %d with no error to show for it", src, res.ExitCode)
		}
	}
}

// The sink is called from two goroutines -- the one copying the script's
// stdout and the one parsing records off its stderr. An HTTP stream writes
// each frame straight to the response, so two calls at once interleave
// bytes and corrupt the chunked encoding ("chunked line ends with bare LF"),
// which is how TestAStreamConsumerCanCancelAfterTheResult lost its end frame
// (#270). One sink call at a time is the contract.
func TestTheSinkIsNeverCalledConcurrently(t *testing.T) {
	needRuntime(t)
	svc := service(t, store(t))
	var inFlight, overlaps, emits, stdouts atomic.Int32
	_, err := svc.RunWith(context.Background(), runner.Options{
		Source: `
import { emit } from "./mcpx-client.ts";
for (let i = 0; i < 200; i++) { console.log("out " + i); emit({ i }); }
`,
		ClientSource: minimalClient,
		Permissions:  "all",
	}, execsvc.Options{}, func(f execsvc.Frame) error {
		switch f.Type {
		case execsvc.FrameEmit:
			emits.Add(1)
		case execsvc.FrameStdout:
			stdouts.Add(1)
		}
		if inFlight.Add(1) > 1 {
			overlaps.Add(1)
		}
		// Widens the window, so an unserialised sink is caught every run
		// rather than occasionally.
		time.Sleep(200 * time.Microsecond)
		inFlight.Add(-1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if emits.Load() < 200 || stdouts.Load() < 2 {
		t.Fatalf("premise: frames must come from both streams; %d emit, %d stdout",
			emits.Load(), stdouts.Load())
	}
	if n := overlaps.Load(); n > 0 {
		t.Fatalf("the sink was entered concurrently %d times", n)
	}
}
