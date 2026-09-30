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

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/dezren39/mcpx/internal/daemon"
)

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
	viewStats
	viewServers
	viewSessions
	viewStorage
)

// views is the switch order, and the order the footer lists them.
var views = []view{viewBrowse, viewLog, viewStats, viewServers, viewSessions, viewStorage}

func (v view) String() string {
	switch v {
	case viewBrowse:
		return "tools"
	case viewLog:
		return "log"
	case viewStats:
		return "stats"
	case viewServers:
		return "servers"
	case viewSessions:
		return "sessions"
	case viewStorage:
		return "storage"
	}
	return "?"
}

// statsDimensions are cycled through within the stats view, so one key
// reaches all of them rather than one key each.
var statsDimensions = []string{"calls", "servers", "errors", "sessions", "slowest", "volume"}

// Styles are resolved once. lipgloss detects colour support at construction,
// so building them per frame would re-probe the terminal on every keystroke.
type styles struct {
	app       lipgloss.Style
	title     lipgloss.Style
	pane      lipgloss.Style
	paneOn    lipgloss.Style
	detail    lipgloss.Style
	status    lipgloss.Style
	selected  lipgloss.Style
	tab       lipgloss.Style
	tabOn     lipgloss.Style
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
		detail: lipgloss.NewStyle().Padding(0, 1),
		status: lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		selected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("231")).Background(lipgloss.Color("62")),
		tab: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		tabOn: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("238")),
		errorText: lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	}
}

type keymap struct {
	Up, Down, Left, Right       key.Binding
	Tab, Enter, Back, Dimension key.Binding
	NextView, PrevView, Refresh key.Binding
	Help, Quit                  key.Binding
}

func newKeymap() keymap {
	return keymap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:      key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "pane left")),
		Right:     key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "pane right")),
		Tab:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Dimension: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dimension")),
		NextView:  key.NewBinding(key.WithKeys("]", "L"), key.WithHelp("]", "next view")),
		PrevView:  key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev view")),
		Refresh:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keymap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextView, k.Enter, k.Refresh, k.Help, k.Quit}
}

func (k keymap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.NextView, k.PrevView, k.Tab, k.Dimension},
		{k.Enter, k.Back, k.Refresh},
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
	body       viewport.Model

	currentNS string
	version   string

	// records backs the log view. Held as records rather than rendered text
	// so that opening one has the values to expand.
	records  []Record
	cursor   int
	table    Table
	statsDim int

	// zoom is the expanded view of one row. Empty means the list is showing.
	zoom string
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
		body:       viewport.New(0, 0),
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
type tableMsg struct {
	view  view
	table Table
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
		rs, err := m.src.Records(m.ctx, 500)
		return logsMsg{items: rs, err: err}
	}
}

// loadTable fetches whichever grid the current view shows.
//
// One message type for five views, because they differ only in the query.
// Five near-identical message types would be five places to forget something.
func (m Model) loadTable(v view) tea.Cmd {
	dim := statsDimensions[m.statsDim]
	return func() tea.Msg {
		var (
			t   Table
			err error
		)
		switch v {
		case viewStats:
			t, err = m.src.Stats(m.ctx, dim)
		case viewServers:
			t, err = m.src.Instances(m.ctx)
		case viewSessions:
			t, err = m.src.Sessions(m.ctx)
		case viewStorage:
			t, err = m.src.Storage(m.ctx)
		}
		return tableMsg{view: v, table: t, err: err}
	}
}

// load fetches whatever the given view needs.
func (m Model) load(v view) tea.Cmd {
	switch v {
	case viewBrowse:
		return m.loadNamespaces()
	case viewLog:
		return m.loadLogs()
	default:
		return m.loadTable(v)
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
		case key.Matches(msg, m.keys.Back):
			// Escape closes an expanded row before it does anything else,
			// because that is the only thing it can have opened.
			if m.zoom != "" {
				m.closeZoom()
				return m, nil
			}
		case key.Matches(msg, m.keys.NextView):
			return m.switchView(1)
		case key.Matches(msg, m.keys.PrevView):
			return m.switchView(-1)
		case key.Matches(msg, m.keys.Dimension):
			if m.view == viewStats {
				m.statsDim = (m.statsDim + 1) % len(statsDimensions)
				m.loading = true
				return m, tea.Batch(m.spin.Tick, m.loadTable(viewStats))
			}
		case key.Matches(msg, m.keys.Enter):
			if cmd := m.open(); cmd != nil {
				return m, cmd
			}
			return m, nil
		case key.Matches(msg, m.keys.Refresh):
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.load(m.view))
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
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.records = msg.items
		// Newest last and the cursor at the end, because the reason to open
		// the log is almost always "what just happened".
		m.cursor = len(m.records) - 1
		m.renderList()
		return m, nil

	case tableMsg:
		m.loading = false
		if msg.view != m.view {
			// A reply for a view already left must not overwrite the current
			// one; holding the switch key makes that ordinary.
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.table = msg.table
		m.cursor = 0
		m.renderList()
		return m, nil

	case tea.MouseMsg:
		if m.view == viewBrowse || m.zoom != "" {
			break
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveCursor(-3)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.moveCursor(3)
			return m, nil
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress {
				return m, nil
			}
			// Rows start below the header and the pane border, so the click
			// maps to a row by subtracting both and adding the scroll.
			row := msg.Y - 2 + m.body.YOffset
			if m.view != viewLog {
				row-- // the table draws a header line of its own
			}
			if row >= 0 && row < m.rows() {
				m.cursor = row
				m.renderList()
				return m, m.open()
			}
			return m, nil
		}

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
	if m.view != viewBrowse {
		if k, ok := msg.(tea.KeyMsg); ok && m.zoom == "" {
			switch {
			case key.Matches(k, m.keys.Down):
				m.moveCursor(1)
				return m, nil
			case key.Matches(k, m.keys.Up):
				m.moveCursor(-1)
				return m, nil
			}
		}
		m.body, c = m.body.Update(msg)
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

// switchView moves through the views and loads the one arrived at.
func (m Model) switchView(delta int) (tea.Model, tea.Cmd) {
	at := 0
	for i, v := range views {
		if v == m.view {
			at = i
			break
		}
	}
	at = (at + delta + len(views)) % len(views)
	m.view = views[at]
	m.zoom = ""
	m.cursor = 0
	m.table = Table{}
	m.loading = true
	m.layout()
	return m, tea.Batch(m.spin.Tick, m.load(m.view))
}

// open expands whatever the cursor is on.
//
// A log line is truncated to fit a terminal, and the part cut off is usually
// the part being looked for -- a stack, a full path, a nested result. Opening
// it is the difference between the log being browsable and being a place to
// notice that something exists before going to another command to read it.
// closeZoom returns to the list.
//
// Clearing the flag is not enough: the viewport still holds the detail, so
// the list has to be drawn back into it. Forgetting that is why escape
// appeared to do nothing.
func (m *Model) closeZoom() {
	m.zoom = ""
	m.renderList()
	m.body.GotoTop()
	m.ensureCursorVisible()
}

func (m *Model) open() tea.Cmd {
	if m.zoom != "" {
		m.closeZoom()
		return nil
	}
	switch m.view {
	case viewLog:
		if m.cursor >= 0 && m.cursor < len(m.records) {
			m.zoom = m.records[m.cursor].Detail()
			m.body.SetContent(m.zoom)
			m.body.GotoTop()
		}
	case viewStats, viewServers, viewSessions, viewStorage:
		if m.cursor < len(m.table.Detail) {
			m.zoom = m.table.Detail[m.cursor]
			m.body.SetContent(m.zoom)
			m.body.GotoTop()
		}
	}
	return nil
}

// moveCursor moves within whichever list is showing and keeps it in view.
func (m *Model) moveCursor(delta int) {
	n := m.rows()
	if n == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	m.renderList()
	m.ensureCursorVisible()
}

// ensureCursorVisible scrolls just enough, rather than recentring, which is
// disorienting when stepping one row at a time.
func (m *Model) ensureCursorVisible() {
	if m.cursor < m.body.YOffset {
		m.body.SetYOffset(m.cursor)
	}
	if bottom := m.body.YOffset + m.body.Height - 2; m.cursor > bottom {
		m.body.SetYOffset(m.cursor - m.body.Height + 2)
	}
}

func (m Model) rows() int {
	if m.view == viewLog {
		return len(m.records)
	}
	return len(m.table.Rows)
}

// renderList draws the current view's list into the body viewport.
func (m *Model) renderList() {
	sel := func(s string) string { return m.styles.selected.Render(s) }
	dim := func(s string) string { return m.styles.dim.Render(s) }

	if m.view == viewLog {
		var b strings.Builder
		for i, r := range m.records {
			line := truncate(r.Line(), m.body.Width)
			if i == m.cursor {
				line = sel(pad(line, m.body.Width))
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		m.body.SetContent(strings.TrimRight(b.String(), "\n"))
		return
	}
	m.body.SetContent(m.table.Render(m.body.Width, m.cursor, sel, dim))
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
	m.body.Width, m.body.Height = m.width-4, body
	m.help.Width = m.width
}

// View renders a frame.
func (m Model) View() string {
	if !m.ready {
		return "\n  starting...\n"
	}
	// Every view is named in the header with the current one marked, so the
	// set is discoverable without opening help. A view nobody knows exists
	// is a view nobody uses.
	tabs := make([]string, 0, len(views))
	for _, v := range views {
		name := v.String()
		if v == m.view {
			if v == viewStats {
				name += ":" + statsDimensions[m.statsDim]
			}
			tabs = append(tabs, m.styles.tabOn.Render(" "+name+" "))
			continue
		}
		tabs = append(tabs, m.styles.tab.Render(" "+name+" "))
	}
	header := m.styles.title.Render(" mcpx "+m.version+" ") + " " + strings.Join(tabs, "")
	if m.loading {
		header += " " + m.spin.View()
	}
	if m.zoom != "" {
		header += "  " + m.styles.dim.Render("(esc closes)")
	}
	if m.err != nil {
		header += "  " + m.styles.errorText.Render(truncate(m.err.Error(), 60))
	}

	var body string
	if m.view != viewBrowse {
		body = m.styles.paneOn.Render(m.body.View())
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
		// Cell motion rather than all motion: it reports clicks and the
		// wheel without streaming an event per pixel of movement.
		//
		// Capturing the mouse does take over text selection. Every terminal
		// worth using restores it on shift-drag, which is the convention, and
		// the footer says so -- being unable to click a log line open is a
		// worse trade than learning one modifier.
		tea.WithMouseCellMotion(),
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

// TableMsgForTest builds a table reply for a given view, so a test can
// deliver a stale one and assert it is dropped.
func TableMsgForTest(v int, t Table) tea.Msg {
	return tableMsg{view: view(v), table: t}
}
