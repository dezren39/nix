// Package tui is the full-screen browser.
//
// It exists for the same reason the prompt-based explorer does -- discovery is
// a loop, and running four commands to go round it once is enough friction
// that people guess instead -- but answers a different shape of question. The
// prompt is better when you know what you want and will paste the result
// somewhere. This is better when you do not: three panes let a namespace, its
// tools and one signature be on screen at once, so comparing two tools is a
// keystroke rather than two commands and a scrollback hunt.
//
// Both are kept. Neither is a worse version of the other, and the prompt still
// works where this cannot run: over a pipe, in CI, inside another program.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dezren39/mcpx/internal/daemon"
)

// Source is what the model reads. An interface rather than the concrete
// client so the view can be driven by a fake in tests, which is the only way
// to test a terminal program without a terminal.
type Source interface {
	Namespaces(ctx context.Context) ([]daemon.NamespaceInfo, error)
	Tools(ctx context.Context, ns string) ([]daemon.ToolInfo, error)
	Signature(ctx context.Context, ns, tool string) (string, error)
	Records(ctx context.Context, limit int) ([]Record, error)
}

// Record is one log line, flattened for display.
type Record struct {
	Time  time.Time
	Level string
	Msg   string
	Attrs string
}

type pane int

const (
	paneNamespaces pane = iota
	paneTools
	paneDetail
)

type view int

const (
	viewBrowse view = iota
	viewLog
)

// Styles are resolved once. lipgloss detects colour support at construction,
// so building them per frame would re-probe the terminal on every keystroke.
type styles struct {
	app       lipgloss.Style
	title     lipgloss.Style
	pane      lipgloss.Style
	paneOn    lipgloss.Style
	detail    lipgloss.Style
	status    lipgloss.Style
	errorText lipgloss.Style
	dim       lipgloss.Style
}

func newStyles() styles {
	border := lipgloss.RoundedBorder()
	return styles{
		title: lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62")).
			Padding(0, 1),
		pane: lipgloss.NewStyle().Border(border).
			BorderForeground(lipgloss.Color("240")),
		paneOn: lipgloss.NewStyle().Border(border).
			BorderForeground(lipgloss.Color("62")),
		detail:    lipgloss.NewStyle().Padding(0, 1),
		status:    lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		errorText: lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	}
}

type keymap struct {
	Left, Right, Tab, Enter, Refresh, Logs, Help, Quit key.Binding
}

func newKeymap() keymap {
	return keymap{
		Left:  key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "pane left")),
		Right: key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "pane right")),
		Tab:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Refresh: key.NewBinding(key.WithKeys("r"),
			key.WithHelp("r", "refresh")),
		Logs: key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "logs")),
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Logs, k.Refresh, k.Help, k.Quit}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Left, k.Right, k.Tab},
		{k.Enter, k.Refresh, k.Logs},
		{k.Help, k.Quit},
	}
}

// item adapts a namespace or tool to the list widget.
type item struct {
	title, desc string
	ns, tool    string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

// Model is the whole application state.
type Model struct {
	src    Source
	ctx    context.Context
	styles styles
	keys   keymap
	help   help.Model
	spin   spinner.Model

	view    view
	focus   pane
	width   int
	height  int
	ready   bool
	loading bool
	err     error

	namespaces list.Model
	tools      list.Model
	detail     viewport.Model
	logs       viewport.Model

	currentNS string
	version   string
}

// New builds the model.
func New(ctx context.Context, src Source, version string) Model {
	s := newStyles()
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	mk := func(title string) list.Model {
		l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
		l.Title = title
		l.SetShowStatusBar(false)
		l.SetFilteringEnabled(true)
		l.Styles.Title = s.title
		// The list's own help would duplicate the application's footer and
		// disagree with it about what the keys do.
		l.SetShowHelp(false)
		return l
	}
	return Model{
		src: ctx2src(ctx, src), ctx: ctx, styles: s, keys: newKeymap(),
		help: help.New(), spin: sp, version: version,
		namespaces: mk("namespaces"),
		tools:      mk("tools"),
		detail:     viewport.New(0, 0),
		logs:       viewport.New(0, 0),
		loading:    true,
	}
}

func ctx2src(_ context.Context, s Source) Source { return s }

// ---- messages ----

type namespacesMsg struct {
	items []daemon.NamespaceInfo
	err   error
}
type toolsMsg struct {
	ns    string
	items []daemon.ToolInfo
	err   error
}
type detailMsg struct {
	text string
	err  error
}
type logsMsg struct {
	items []Record
	err   error
}

func (m Model) loadNamespaces() tea.Cmd {
	return func() tea.Msg {
		ns, err := m.src.Namespaces(m.ctx)
		return namespacesMsg{items: ns, err: err}
	}
}

func (m Model) loadTools(ns string) tea.Cmd {
	return func() tea.Msg {
		ts, err := m.src.Tools(m.ctx, ns)
		return toolsMsg{ns: ns, items: ts, err: err}
	}
}

func (m Model) loadDetail(ns, tool string) tea.Cmd {
	return func() tea.Msg {
		text, err := m.src.Signature(m.ctx, ns, tool)
		return detailMsg{text: text, err: err}
	}
}

func (m Model) loadLogs() tea.Cmd {
	return func() tea.Msg {
		rs, err := m.src.Records(m.ctx, 300)
		return logsMsg{items: rs, err: err}
	}
}

// Init starts the first load.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.loadNamespaces())
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true

	case tea.KeyMsg:
		// While a filter is open every key belongs to it, including q. Losing
		// that check means typing a namespace containing "q" quits.
		if m.namespaces.FilterState() == list.Filtering ||
			m.tools.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			m.layout()
			return m, nil
		case key.Matches(msg, m.keys.Logs):
			if m.view == viewLog {
				m.view = viewBrowse
				return m, nil
			}
			m.view = viewLog
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadLogs())
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			if m.view == viewLog {
				return m, tea.Batch(m.spin.Tick, m.loadLogs())
			}
			return m, tea.Batch(m.spin.Tick, m.loadNamespaces())
		case key.Matches(msg, m.keys.Tab):
			m.focus = (m.focus + 1) % 3
			return m, m.syncSelection()
		case key.Matches(msg, m.keys.Left):
			if m.focus > paneNamespaces {
				m.focus--
			}
			return m, nil
		case key.Matches(msg, m.keys.Right):
			if m.focus < paneDetail {
				m.focus++
			}
			return m, m.syncSelection()
		}

	case namespacesMsg:
		m.loading = false
		m.err = msg.err
		items := make([]list.Item, 0, len(msg.items))
		for _, n := range msg.items {
			desc := n.Description
			if desc == "" {
				desc = n.Server
			}
			state := fmt.Sprintf("%d tools", n.Tools)
			if n.Live > 0 {
				state += fmt.Sprintf(", %d live", n.Live)
			}
			if n.Error != "" {
				state = "error: " + n.Error
			}
			items = append(items, item{
				title: n.Namespace, desc: state + " \u00b7 " + desc, ns: n.Namespace,
			})
		}
		m.namespaces.SetItems(items)
		return m, m.syncSelection()

	case toolsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// A stale reply for a namespace the cursor has already left must not
		// overwrite the current one; holding a key down makes that ordinary.
		if msg.ns != m.currentNS {
			return m, nil
		}
		items := make([]list.Item, 0, len(msg.items))
		for _, t := range msg.items {
			items = append(items, item{
				title: t.Tool, desc: firstLine(t.Description), ns: t.Namespace, tool: t.Tool,
			})
		}
		m.tools.SetItems(items)
		return m, m.syncDetail()

	case detailMsg:
		m.loading = false
		if msg.err != nil {
			m.detail.SetContent(m.styles.errorText.Render(msg.err.Error()))
			return m, nil
		}
		m.detail.SetContent(msg.text)
		m.detail.GotoTop()
		return m, nil

	case logsMsg:
		m.loading = false
		if msg.err != nil {
			m.logs.SetContent(m.styles.errorText.Render(msg.err.Error()))
			return m, nil
		}
		var b strings.Builder
		for _, r := range msg.items {
			fmt.Fprintf(&b, "%s %-5s %s %s\n",
				r.Time.Format("15:04:05.000"), r.Level, r.Msg,
				m.styles.dim.Render(r.Attrs))
		}
		m.logs.SetContent(strings.TrimRight(b.String(), "\n"))
		m.logs.GotoBottom()
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var c tea.Cmd
			m.spin, c = m.spin.Update(msg)
			return m, c
		}
		return m, nil
	}

	// Route to whichever widget has focus.
	var c tea.Cmd
	if m.view == viewLog {
		m.logs, c = m.logs.Update(msg)
		return m, c
	}
	switch m.focus {
	case paneNamespaces:
		before := m.namespaces.Index()
		m.namespaces, c = m.namespaces.Update(msg)
		cmds = append(cmds, c)
		if m.namespaces.Index() != before {
			cmds = append(cmds, m.syncSelection())
		}
	case paneTools:
		before := m.tools.Index()
		m.tools, c = m.tools.Update(msg)
		cmds = append(cmds, c)
		if m.tools.Index() != before {
			cmds = append(cmds, m.syncDetail())
		}
	case paneDetail:
		m.detail, c = m.detail.Update(msg)
		cmds = append(cmds, c)
	}
	return m, tea.Batch(cmds...)
}

// syncSelection loads the tools for whatever namespace is selected.
func (m *Model) syncSelection() tea.Cmd {
	it, ok := m.namespaces.SelectedItem().(item)
	if !ok || it.ns == m.currentNS {
		return nil
	}
	m.currentNS = it.ns
	m.tools.SetItems(nil)
	m.detail.SetContent("")
	return m.loadTools(it.ns)
}

func (m *Model) syncDetail() tea.Cmd {
	it, ok := m.tools.SelectedItem().(item)
	if !ok {
		return nil
	}
	return m.loadDetail(it.ns, it.tool)
}

// layout recomputes pane sizes.
//
// Done on resize rather than per frame: the arithmetic is cheap but the
// widgets reflow their contents when told a new size, and doing that during
// a render is how a list loses its scroll position.
func (m *Model) layout() {
	if m.width == 0 {
		return
	}
	helpHeight := 1
	if m.help.ShowAll {
		helpHeight = 3
	}
	body := m.height - helpHeight - 2
	if body < 4 {
		body = 4
	}

	left := m.width / 4
	if left < 18 {
		left = 18
	}
	mid := m.width / 4
	if mid < 18 {
		mid = 18
	}
	right := m.width - left - mid - 8
	if right < 20 {
		right = 20
	}

	m.namespaces.SetSize(left, body)
	m.tools.SetSize(mid, body)
	m.detail.Width, m.detail.Height = right, body
	m.logs.Width, m.logs.Height = m.width-4, body
	m.help.Width = m.width
}

// View renders a frame.
func (m Model) View() string {
	if !m.ready {
		return "\n  starting...\n"
	}
	header := m.styles.title.Render(" mcpx " + m.version + " ")
	if m.loading {
		header += " " + m.spin.View()
	}
	if m.err != nil {
		header += "  " + m.styles.errorText.Render(m.err.Error())
	}

	var body string
	if m.view == viewLog {
		body = m.styles.paneOn.Render(m.logs.View())
	} else {
		frame := func(p pane, s string) string {
			if m.focus == p {
				return m.styles.paneOn.Render(s)
			}
			return m.styles.pane.Render(s)
		}
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			frame(paneNamespaces, m.namespaces.View()),
			frame(paneTools, m.tools.View()),
			frame(paneDetail, m.styles.detail.Render(m.detail.View())),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, m.help.View(m.keys))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// Run starts the program.
func Run(ctx context.Context, src Source, version string) error {
	p := tea.NewProgram(New(ctx, src, version),
		tea.WithAltScreen(),
		// Mouse support is off. A full-screen program that captures the mouse
		// breaks terminal text selection, and losing the ability to copy a
		// tool signature costs more than scroll-wheel scrolling is worth.
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	return err
}

// ToolsMsgForTest builds a tools reply, so a test can deliver a stale one and
// assert it is dropped. Exported only for that; the message type itself stays
// unexported because nothing outside should be constructing state updates.
func ToolsMsgForTest(ns string, items []daemon.ToolInfo) tea.Msg {
	return toolsMsg{ns: ns, items: items}
}
