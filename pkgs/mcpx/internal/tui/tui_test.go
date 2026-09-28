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
		{Time: time.Now(), Level: "INFO", Msg: "a thing happened", Attrs: "k=v"},
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

func TestTheLogViewIsReachableAndComesBack(t *testing.T) {
	f := newFake()
	m := drive(t, tui.New(context.Background(), f, "test"), key("L"))
	if f.calls["records"] == 0 {
		t.Fatal("entering the log view should load records")
	}
	if !strings.Contains(m.View(), "a thing happened") {
		t.Errorf("records should be shown:\n%s", m.View())
	}
	m = drive(t, m, key("L"))
	if !strings.Contains(m.View(), "namespaces") {
		t.Errorf("pressing it again should return to browsing:\n%s", m.View())
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
