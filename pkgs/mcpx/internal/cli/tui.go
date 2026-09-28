package cli

import (
	"context"
	"errors"
	"fmt"
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
		keys := make([]string, 0, len(r.Attrs))
		for k := range r.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s=%v ", k, r.Attrs[k])
		}
		out = append(out, tui.Record{
			Time:  r.Time,
			Level: strings.ToUpper(r.Level.String()),
			Msg:   r.Msg,
			Attrs: strings.TrimSpace(b.String()),
		})
	}
	return out, nil
}

// CmdTUI runs the full-screen browser.
func (a *App) CmdTUI(ctx context.Context, args []string) error {
	fs := newFlagSet("tui")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if !isTerminal(os.Stdout) || !isTerminal(os.Stdin) {
		return errors.New("the tui needs a terminal; " +
			"use `mcpx explore` for a prompt, or ls/types/catalog/log for scripting")
	}
	return tui.Run(ctx, tuiSource{app: a}, a.Version)
}
