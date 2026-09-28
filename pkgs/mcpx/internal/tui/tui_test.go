package tui_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/tui"
)

// fake is why Source is an interface: a full-screen program cannot be driven
// by a terminal in a test, and one that cannot be tested breaks quietly.
type fake struct {
	nsErr    error
	toolsErr error
	calls    map[string]int
}

func newFake() *fake { return &fake{calls: map[string]int{}} }

func (f *fake) Namespaces(context.Context) ([]daemon.NamespaceInfo, error) {
	f.calls["namespaces"]++
	if f.nsErr != nil {
		return nil, f.nsErr
	}
	return []daemon.NamespaceInfo{
		{Namespace: "alpha", Server: "a", Tools: 2, Description: "first"},
		{Namespace: "beta", Server: "b", Tools: 1, Live: 1},
	}, nil
}

func (f *fake) Tools(_ context.Context, ns string) ([]daemon.ToolInfo, error) {
	f.calls["tools:"+ns]++
	if f.toolsErr != nil {
		return nil, f.toolsErr
	}
	return []daemon.ToolInfo{
		{Namespace: ns, Tool: "read", Description: "read a thing\nsecond line"},
		{Namespace: ns, Tool: "write", Description: "write a thing"},
	}, nil
}

func (f *fake) Signature(_ context.Context, ns, tool string) (string, error) {
	f.calls["sig:"+ns+"."+tool]++
	return "function " + ns + "_" + tool + "(): Promise<void>;", nil
}

func (f *fake) Records(context.Context, int) ([]tui.Record, error) {
	f.calls["records"]++
	return []tui.Record{
		{Time: time.Now(), Level: "INFO", Msg: "a thing happened",
			Attrs: map[string]any{"k": "v"}},
		{Time: time.Now(), Level: "ERROR", Msg: "it went wrong",
			Attrs: map[string]any{"stack": "line one\nline two", "code": 7.0}},
	}, nil
}

func (f *fake) Stats(_ context.Context, dim string) (tui.Table, error) {
	f.calls["stats:"+dim]++
	return tui.Table{
		Title: dim, Columns: []string{"A", "B"},
		Rows:   [][]string{{"one", "1"}, {"two", "2"}},
		Detail: []string{"detail for one", "detail for two"},
	}, nil
}

func (f *fake) Instances(context.Context) (tui.Table, error) {
	f.calls["instances"]++
	return tui.Table{
		Columns: []string{"SERVER", "PID"},
		Rows:    [][]string{{"alpha", "123"}},
		Detail:  []string{"alpha instance detail"},
	}, nil
}

func (f *fake) Sessions(context.Context) (tui.Table, error) {
	f.calls["sessions"]++
	return tui.Table{
		Columns: []string{"SESSION"}, Rows: [][]string{{"s-1"}},
		Detail: []string{"session detail"},
	}, nil
}

func (f *fake) Storage(context.Context) (tui.Table, error) {
	f.calls["storage"]++
	return tui.Table{
		Columns: []string{"WHAT", "SIZE"},
		Rows:    [][]string{{"index", "1.2 MiB"}},
		Detail:  []string{"index detail"}, Note: "total 1.2 MiB",
	}, nil
}

// drive feeds messages to the model and returns the final one, running any
// command each step produces so loads actually complete.
func drive(t *testing.T, m tea.Model, msgs ...tea.Msg) tea.Model {
	t.Helper()
	var step func(tea.Msg)
	// Commands are run and their messages fed back, which is what bubbletea's
	// runtime does. Without it Init never fires and nothing loads -- the model
	// renders an empty frame and every assertion fails for the wrong reason.
	//
	// Each one is given a short deadline rather than being waited on. The
	// spinner's tick is a timer, so running it to completion made every test
	// pay its interval for a frame nobody looks at.
	settle := func(cmd tea.Cmd) tea.Msg {
		if cmd == nil {
			return nil
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case msg := <-done:
			return msg
		case <-time.After(150 * time.Millisecond):
			return nil
		}
	}
	run := func(cmd tea.Cmd) {
		produced := settle(cmd)
		if produced == nil {
			return
		}
		if batch, ok := produced.(tea.BatchMsg); ok {
			for _, c := range batch {
				if inner := settle(c); inner != nil {
					step(inner)
				}
			}
			return
		}
		step(produced)
	}
	step = func(msg tea.Msg) {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		for i := 0; cmd != nil && i < 6; i++ {
			produced := settle(cmd)
			if produced == nil {
				break
			}
			if batch, ok := produced.(tea.BatchMsg); ok {
				for _, c := range batch {
					if inner := settle(c); inner != nil {
						m, _ = m.Update(inner)
					}
				}
				break
			}
			m, cmd = m.Update(produced)
		}
	}
	run(m.Init())
	step(tea.WindowSizeMsg{Width: 120, Height: 40})
	for _, msg := range msgs {
		step(msg)
	}
	return m
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNamespacesAndTheirToolsAppear(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"))
	view := m.View()
	for _, want := range []string{"alpha", "beta", "namespaces", "tools"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q missing from the view:\n%s", want, view)
		}
	}
}

func TestSelectingANamespaceLoadsItsTools(t *testing.T) {
	// The whole point of three panes: the tools appear without a second
	// command.
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"))
	if f.calls["tools:alpha"] == 0 {
		t.Fatal("the first namespace's tools should load on their own")
	}
	if !strings.Contains(m.View(), "read") {
		t.Errorf("tools should be visible:\n%s", m.View())
	}
}

func TestTheSignatureLoadsForTheSelectedTool(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"))
	if f.calls["sig:alpha.read"] == 0 {
		t.Fatal("the selected tool's signature should load")
	}
	if !strings.Contains(m.View(), "alpha_read") {
		t.Errorf("the signature should be shown:\n%s", m.View())
	}
}

func TestMovingBetweenNamespacesLoadsTheNewOne(t *testing.T) {
	f := newFake()
	drive(t, tui.New(context.Background(), f, "test"), key("down"))
	if f.calls["tools:beta"] == 0 {
		t.Error("moving the cursor should load the newly selected namespace")
	}
}

func TestAStaleReplyDoesNotOverwriteTheCurrentSelection(t *testing.T) {
	// Holding a key down makes out-of-order replies ordinary, and a late one
	// clobbering the current pane is the bug that produces "the tools are
	// for the wrong server sometimes".
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("down"))
	m, _ = m.Update(tui.ToolsMsgForTest("alpha", []daemon.ToolInfo{
		{Namespace: "alpha", Tool: "STALE"},
	}))
	if strings.Contains(m.View(), "STALE") {
		t.Errorf("a reply for a namespace the cursor has left must be dropped:\n%s", m.View())
	}
}

func TestTheLogViewIsReachable(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"))
	if f.calls["records"] == 0 {
		t.Fatal("entering the log view should load records")
	}
	if !strings.Contains(m.View(), "a thing happened") {
		t.Errorf("records should be shown:\n%s", m.View())
	}
}

func TestEveryViewIsReachableAndLoadsItsOwnData(t *testing.T) {
	// A view nobody knows exists is a view nobody uses, so they are all named
	// in the header; this asserts each one actually fetches something.
	f := newFake()
	m := tui.Model(tui.New(context.Background(), f, "test"))
	var model tea.Model = m
	model = drive(t, model)
	for _, want := range []string{"records", "stats:calls", "instances", "sessions", "storage"} {
		model = drive(t, model, key("]"))
		if f.calls[want] == 0 {
			t.Errorf("%s was never loaded; the view did not fetch", want)
		}
	}
	// And back round to the start.
	model = drive(t, model, key("]"))
	if !strings.Contains(model.View(), "namespaces") {
		t.Errorf("the views should cycle:\n%s", model.View())
	}
}

func TestEnterOpensALogRecordAndEscapeCloses(t *testing.T) {
	// A log line is truncated to fit, and the part cut off is usually what is
	// being looked for. Opening it is the difference between the log being
	// browsable and being a place to notice something before going elsewhere.
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"))
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	view := m.View()
	if !strings.Contains(view, "line one") || !strings.Contains(view, "line two") {
		t.Errorf("a multi-line attribute should be expanded:\n%s", view)
	}
	if !strings.Contains(view, "esc closes") {
		t.Errorf("the way out should be stated:\n%s", view)
	}
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	// Checked on the indicator rather than the text: a flattened row
	// legitimately contains the escaped "line one\nline two", so looking for
	// that would fail whether or not escape worked.
	after := m.View()
	if strings.Contains(after, "esc closes") {
		t.Errorf("escape should return to the list:\n%s", after)
	}
	if !strings.Contains(after, "a thing happened") {
		t.Errorf("the list should be back:\n%s", after)
	}
}

func TestEnterOpensATableRow(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"), key("]"))
	m = drive(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(m.View(), "detail for one") {
		t.Errorf("the selected row should expand:\n%s", m.View())
	}
}

func TestDCyclesTheStatsDimension(t *testing.T) {
	// One key for six statistics, rather than six keys.
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"), key("]"))
	if f.calls["stats:calls"] == 0 {
		t.Fatal("stats should open on calls")
	}
	m = drive(t, m, key("d"))
	if f.calls["stats:servers"] == 0 {
		t.Errorf("d should move to the next dimension: %v", f.calls)
	}
	if !strings.Contains(m.View(), "stats:servers") {
		t.Errorf("the header should say which one:\n%s", m.View())
	}
}

func TestAStaleTableReplyIsDropped(t *testing.T) {
	// Holding the view-switch key makes out-of-order replies ordinary.
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"), key("]"))
	m, _ = m.Update(tui.TableMsgForTest(5, tui.Table{
		Columns: []string{"X"}, Rows: [][]string{{"STALE"}},
	}))
	if strings.Contains(m.View(), "STALE") {
		t.Errorf("a reply for another view must be dropped:\n%s", m.View())
	}
}

func TestClickingALogLineOpensIt(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("]"))
	m = drive(t, m, tea.MouseMsg{
		X: 4, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if !strings.Contains(m.View(), "a thing happened") {
		t.Errorf("a click should select and open a row:\n%s", m.View())
	}
}

func TestAFailureIsShownRatherThanHidden(t *testing.T) {
	f := newFake()
	f.nsErr = errors.New("daemon is not running")
	m := drive(t, tui.New(context.Background(), f, "test"))
	if !strings.Contains(m.View(), "daemon is not running") {
		t.Errorf("the error should be on screen:\n%s", m.View())
	}
}

func TestQuitStops(t *testing.T) {
	f := newFake()
	m := tui.New(context.Background(), f, "test")
	m2, cmd := m.Update(key("q"))
	_ = m2
	if cmd == nil {
		t.Fatal("q should produce a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
}

func TestKeysGoToTheFilterWhileItIsOpen(t *testing.T) {
	// Without this check, typing a namespace containing "q" quits.
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("/"))
	_, cmd := m.Update(key("q"))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Error("q while filtering should be text, not a command")
		}
	}
}
