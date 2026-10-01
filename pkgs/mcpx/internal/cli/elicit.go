package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dezren39/mcpx/internal/elicit"
)

// ExitInputRequired is returned when a call stopped to ask something.
//
// 75 is EX_TEMPFAIL, which is what this is: not a failure, a "try again when
// you have an answer". A distinct code matters because the alternative is a
// caller parsing stdout to find out whether it succeeded.
//
// `mcpx call` exits with it when the tool's server asks a question mid-call
// (InputRequiredError); a script run by `mcpx exec` or `mcpx run` exits with
// it from inside the generated client, which sees the same answer from the
// daemon. Either way the call keeps running on the daemon until the question
// is answered or expires.
const ExitInputRequired = 75

// ExitCoder is an error that decides the process's exit status.
type ExitCoder interface {
	error
	ExitCode() int
}

// broker opens the question store, which lives beside the log index.
func (a *App) broker() (*elicit.Broker, error) {
	dir := firstNonEmpty(a.Settings().String("logging.dir"),
		filepath.Join(a.Paths.State, "logs"))
	if err := os.MkdirAll(dir, defaults.PublicDirMode); err != nil {
		return nil, err
	}
	return elicit.Open(filepath.Join(dir, "elicit.db"))
}

// CmdElicit answers, lists and follows outstanding questions.
func (a *App) CmdElicit(ctx context.Context, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	args = hoistFlags(args, map[string]bool{"session": true, "audience": true})
	fs := newFlagSet("elicit")
	session := fs.String("session", "", "restrict to one session")
	audience := fs.String("audience", "", "agent or human")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	b, err := a.broker()
	if err != nil {
		return err
	}
	defer b.Close()

	switch sub {
	case "", "list":
		pending, err := b.Pending(elicit.Filter{
			Session: *session, Audience: elicit.Audience(*audience),
		})
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(pending)
		}
		if len(pending) == 0 {
			fmt.Println("Nothing is waiting for an answer.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tFOR\tLEFT\tSERVER\tASKS")
		for _, r := range pending {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Audience,
				short(r.TTLRemaining()), orDash(r.Server), trunc(r.Message, 48))
		}
		_ = w.Flush()
		fmt.Println("\n  mcpx elicit answer <id> '<json>' | decline <id> | cancel <id>")
		return nil

	case "show":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx elicit show <id>")
		}
		r, ok, err := b.Get(fs.Arg(0))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("no elicitation %s", fs.Arg(0))
		}
		if a.JSON {
			return a.out(r)
		}
		fmt.Print(renderQuestion(r))
		return nil

	case "answer", "accept":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx elicit answer <id> '<json>'")
		}
		var content json.RawMessage
		if fs.NArg() > 1 {
			raw := strings.Join(fs.Args()[1:], " ")
			// key=value is accepted as well as JSON, because most answers
			// are one short string and making somebody quote JSON for that
			// is ceremony.
			if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
				pairs := map[string]any{}
				for _, kv := range fs.Args()[1:] {
					k, v, ok := strings.Cut(kv, "=")
					if !ok {
						return fmt.Errorf("arguments are JSON or key=value, got %q", kv)
					}
					pairs[k] = v
				}
				b2, _ := json.Marshal(pairs)
				content = b2
			} else {
				if !json.Valid([]byte(raw)) {
					return fmt.Errorf("the answer is not valid JSON")
				}
				content = json.RawMessage(raw)
			}
		}
		return b.Respond(elicit.Answer{
			ID: fs.Arg(0), Action: elicit.Accept, Content: content, By: "cli",
		})

	case "decline":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx elicit decline <id>")
		}
		return b.Respond(elicit.Answer{ID: fs.Arg(0), Action: elicit.Decline, By: "cli"})

	case "cancel":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx elicit cancel <id>")
		}
		return b.Respond(elicit.Answer{ID: fs.Arg(0), Action: elicit.Cancel, By: "cli"})

	case "watch":
		return a.watchElicit(ctx, b, *session)

	case "result":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx elicit result <id>")
		}
		ans, ok, err := b.Lookup(fs.Arg(0))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s has not been answered", fs.Arg(0))
		}
		return a.out(ans)
	}
	return fmt.Errorf("no elicit subcommand %q; list, show, answer, decline, cancel, watch or result", sub)
}

// watchElicit follows pending questions on a terminal.
//
// Polling rather than subscribing, for now, because the interval that matters
// is a person noticing -- and a subscription that has to survive a daemon
// restart is more machinery than this earns until something depends on it.
func (a *App) watchElicit(ctx context.Context, b *elicit.Broker, session string) error {
	seen := map[string]bool{}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	fmt.Println("Watching for questions. Ctrl-C to stop.")
	for {
		pending, err := b.Pending(elicit.Filter{Session: session})
		if err != nil {
			return err
		}
		for _, r := range pending {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			fmt.Print("\n" + renderQuestion(r))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// renderQuestion prints a question and how to answer it.
//
// The command is spelled out because the alternative is the reader
// assembling it from three fields, getting it wrong once, and then copying
// that mistake forever.
func renderQuestion(r elicit.Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", r.Message)
	fmt.Fprintf(&b, "  id       %s\n", r.ID)
	if r.Server != "" {
		fmt.Fprintf(&b, "  from     %s", r.Server)
		if r.Tool != "" {
			fmt.Fprintf(&b, ".%s", r.Tool)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "  for      %s (%s)\n", r.Audience, r.Reason)
	fmt.Fprintf(&b, "  expires  in %s\n", short(r.TTLRemaining()))
	if r.Mode == elicit.URL {
		fmt.Fprintf(&b, "  url      %s\n", r.URL)
		fmt.Fprintf(&b, "\n  mcpx elicit accept %s     (after visiting it)\n", r.ID)
		fmt.Fprintf(&b, "  mcpx elicit decline %s\n", r.ID)
		return b.String()
	}
	for _, f := range describeSchema(r.Schema) {
		fmt.Fprintf(&b, "  %-8s %s\n", f.name, f.detail)
	}
	fmt.Fprintf(&b, "\n  mcpx elicit answer %s '%s'\n", r.ID, exampleFor(r.Schema))
	fmt.Fprintf(&b, "  mcpx elicit decline %s\n", r.ID)
	return b.String()
}

type schemaField struct{ name, detail string }

func describeSchema(raw json.RawMessage) []schemaField {
	if len(raw) == 0 {
		return nil
	}
	var doc struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
			Enum        []any  `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	required := map[string]bool{}
	for _, r := range doc.Required {
		required[r] = true
	}
	var out []schemaField
	for name, p := range doc.Properties {
		detail := p.Type
		if !required[name] {
			detail += ", optional"
		}
		if len(p.Enum) > 0 {
			var opts []string
			for _, e := range p.Enum {
				opts = append(opts, fmt.Sprint(e))
			}
			detail += ": " + strings.Join(opts, " | ")
		}
		if p.Description != "" {
			detail += " -- " + p.Description
		}
		out = append(out, schemaField{name: name, detail: detail})
	}
	return out
}

func exampleFor(raw json.RawMessage) string {
	fields := describeSchema(raw)
	if len(fields) == 0 {
		return "{}"
	}
	var parts []string
	for _, f := range fields {
		parts = append(parts, fmt.Sprintf("%q:%q", f.name, "..."))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func short(d time.Duration) string {
	switch {
	case d <= 0:
		return "expired"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
