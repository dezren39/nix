package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/opencode"
)

// OpencodeDimensions are the statistics available from an opencode database.
var OpencodeDimensions = []string{"overview", "agents", "models", "projects", "busiest", "activity"}

// statsOpencode reports on opencode's own database.
//
// The numbers worth knowing about an agent's behaviour -- what it cost, which
// model answered, how much of the work was a subagent's -- live in opencode's
// database and are pruned over time. mcpx already keeps a durable log of its
// own, so folding these in beside it means one place to ask rather than two.
//
// The database is opened read-only and belongs to a program that may be
// running. Every statistic checks for the columns it needs, so a schema that
// has moved costs one number rather than the whole command.
func (a *App) statsOpencode(dim, dbPath, since, until string, top int) error {
	path, err := opencode.Find(dbPath)
	if err != nil {
		return err
	}
	db, err := opencode.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now()
	from, err := logstore.ParseWhen(since, now)
	if err != nil {
		return err
	}
	to, err := logstore.ParseWhen(until, now)
	if err != nil {
		return err
	}
	if dim == "" {
		dim = "overview"
	}

	switch dim {
	case "overview":
		o, err := db.Overview(from, to)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(o)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "database\t%s\n", o.Path)
		fmt.Fprintf(w, "size\t%s\n", humanBytes(o.SizeBytes))
		fmt.Fprintf(w, "sessions\t%d\n", o.Sessions)
		fmt.Fprintf(w, "messages\t%d\n", o.Messages)
		fmt.Fprintf(w, "parts\t%d\n", o.Parts)
		fmt.Fprintf(w, "projects\t%d\n", o.Projects)
		if !o.Earliest.IsZero() {
			fmt.Fprintf(w, "span\t%s to %s\n",
				o.Earliest.Format("2006-01-02"), o.Latest.Format("2006-01-02"))
		}
		fmt.Fprintf(w, "cost\t$%.2f\n", o.TotalCost)
		fmt.Fprintf(w, "tokens in\t%s\n", humanCount(o.TokensIn))
		fmt.Fprintf(w, "tokens out\t%s\n", humanCount(o.TokensOut))
		if o.TokensThink > 0 {
			fmt.Fprintf(w, "reasoning\t%s\n", humanCount(o.TokensThink))
		}
		if o.CacheRead > 0 || o.CacheWrite > 0 {
			fmt.Fprintf(w, "cache r/w\t%s / %s\n", humanCount(o.CacheRead), humanCount(o.CacheWrite))
		}
		return w.Flush()

	case "agents", "models", "projects":
		var groups []opencode.Group
		switch dim {
		case "agents":
			groups, err = db.ByAgent(from, to, top)
		case "models":
			groups, err = db.ByModel(from, to, top)
		default:
			groups, err = db.ByProject(from, to, top)
		}
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(groups)
		}
		if len(groups) == 0 {
			fmt.Printf("No %s recorded in that window.\n", dim)
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, strings.ToUpper(dim[:len(dim)-1])+"\tSESSIONS\tCOST\tTOKENS IN\tTOKENS OUT")
		for _, g := range groups {
			fmt.Fprintf(w, "%s\t%d\t$%.2f\t%s\t%s\n", trunc(g.Key, 46), g.Sessions, g.Cost,
				humanCount(g.TokensIn), humanCount(g.TokensOut))
		}
		return w.Flush()

	case "busiest":
		ss, err := db.Busiest(from, to, top)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(ss)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "MESSAGES\tCOST\tAGENT\tMODEL\tCREATED\tTITLE")
		for _, s := range ss {
			fmt.Fprintf(w, "%d\t$%.2f\t%s\t%s\t%s\t%s\n", s.Messages, s.Cost,
				orDash(s.Agent), trunc(orDash(s.Model), 28),
				s.Created.Format("01-02 15:04"), trunc(s.Title, 40))
		}
		return w.Flush()

	case "activity":
		bs, err := db.Activity(from, to, top)
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(bs)
		}
		var peak int64
		for _, b := range bs {
			if b.Sessions > peak {
				peak = b.Sessions
			}
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "HOUR\tSESSIONS\tMESSAGES\t")
		for _, b := range bs {
			fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", b.When.Format("01-02 15:04"),
				b.Sessions, b.Messages, bar(b.Sessions, peak, 24))
		}
		return w.Flush()
	}
	return fmt.Errorf("no opencode statistic named %q; available: %s",
		dim, strings.Join(OpencodeDimensions, ", "))
}

func bar(v, max int64, width int) string {
	if max <= 0 {
		return ""
	}
	n := int(float64(v) / float64(max) * float64(width))
	return strings.Repeat("#", n)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "\u2026"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
