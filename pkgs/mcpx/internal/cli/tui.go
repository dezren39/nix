package cli

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/tui"
)

// tuiSource adapts the daemon client to what the view needs.
//
// The view does not import the client, so it can be driven by a fake. That is
// the only way to test a full-screen program without a terminal, and a
// terminal program nobody can test is one that breaks quietly.
type tuiSource struct {
	app *App
}

func (s tuiSource) Namespaces(ctx context.Context) ([]daemon.NamespaceInfo, error) {
	c, err := s.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.ensureAnySchemas(ctx, c); err != nil {
		return nil, err
	}
	return c.Namespaces(ctx, s.app.Profile)
}

func (s tuiSource) Tools(ctx context.Context, ns string) ([]daemon.ToolInfo, error) {
	c, err := s.app.ensure(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.app.ensureSchemas(ctx, c, []string{ns}); err != nil {
		return nil, err
	}
	ts, err := c.Tools(ctx, []string{ns})
	if err != nil {
		return nil, err
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Tool < ts[j].Tool })
	return ts, nil
}

func (s tuiSource) Signature(ctx context.Context, ns, tool string) (string, error) {
	c, err := s.app.ensure(ctx)
	if err != nil {
		return "", err
	}
	// The single-tool form, which is the whole point of the pane: a full
	// namespace is thousands of characters and one signature is hundreds.
	return c.Types(ctx, []string{ns + "." + tool}, true, s.app.Profile)
}

func (s tuiSource) Records(ctx context.Context, limit int) ([]tui.Record, error) {
	st, err := s.app.openStore("")
	if err != nil {
		return nil, err
	}
	defer st.Close()
	recs, err := st.Records(logstore.Query{Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]tui.Record, 0, len(recs))
	for _, r := range recs {
		out = append(out, tui.Record{
			Time:  r.Time,
			Level: strings.ToUpper(r.Level.String()),
			Msg:   r.Msg,
			Attrs: r.Attrs,
		})
	}
	return out, nil
}

// dumpViews renders every view as data.
func (a *App) dumpViews(ctx context.Context) error {
	src := tuiSource{app: a}
	type entry struct {
		Columns []string   `json:"columns,omitempty"`
		Rows    [][]string `json:"rows,omitempty"`
		Note    string     `json:"note,omitempty"`
		Error   string     `json:"error,omitempty"`
	}
	out := map[string]entry{}
	add := func(name string, t tui.Table, err error) {
		if err != nil {
			out[name] = entry{Error: err.Error()}
			return
		}
		out[name] = entry{Columns: t.Columns, Rows: t.Rows, Note: t.Note}
	}
	for _, dim := range []string{"calls", "servers", "errors", "sessions", "slowest", "volume"} {
		t, err := src.Stats(ctx, dim)
		add("stats:"+dim, t, err)
	}
	t, err := src.Instances(ctx)
	add("servers", t, err)
	t, err = src.Sessions(ctx)
	add("sessions", t, err)
	t, err = src.Storage(ctx)
	add("storage", t, err)
	return a.out(out)
}

// CmdTUI runs the full-screen browser.
// applyColour makes output.color mean something.
//
// It sets the two variables the rendering stack already reads rather than
// reaching into lipgloss for a colour profile. termenv, underneath lipgloss,
// honours NO_COLOR and CLICOLOR_FORCE by convention and so does nearly
// everything else a terminal runs; going through them costs no new dependency
// and, because the variables are inherited, means a script mcpx launches from
// the browser draws the same way. Setting the profile directly would have
// done neither.
//
// Auto sets nothing: the absence of both variables is what auto means.
func applyColour(mode string) {
	switch mode {
	case "never":
		os.Setenv("NO_COLOR", "1")
		os.Unsetenv("CLICOLOR_FORCE")
	case "always":
		os.Setenv("CLICOLOR_FORCE", "1")
		os.Unsetenv("NO_COLOR")
	}
}

func (a *App) CmdTUI(ctx context.Context, args []string) error {
	fs := newFlagSet("tui")
	dump := fs.Bool("dump", false,
		"print every view as JSON and exit, without drawing anything")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	// A terminal program's data can be tested without a terminal, and
	// should be: the fake proves the view reacts, this proves the queries
	// behind it return something.
	if *dump {
		return a.dumpViews(ctx)
	}
	if !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
		return errors.New("the tui needs a terminal; " +
			"use `mcpx explore` for a prompt, or ls/types/catalog/log for scripting")
	}
	applyColour(a.Settings().String("output.color"))
	return tui.Run(ctx, tuiSource{app: a}, a.Version)
}
