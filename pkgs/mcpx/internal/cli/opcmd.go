package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/settings"
)

// opGroup is one generated command: every /v1 operation, not covered by a
// hand-written command, whose CLI words share a first word.
//
// Grouping by the first word is what turns task_get, task_result and
// task_cancel into `mcpx task get|result|cancel` rather than three
// top-level commands nobody would guess the spelling of.
type opGroup struct {
	Name string
	// Aliases are the other first words the operations' own names use --
	// "artifacts" for artifacts_list -- so the spelling in the ops table
	// works on the command line too.
	Aliases []string
	// Bare is the operation reached by the name alone: `mcpx resolve`.
	Bare *api.Op
	// Verbs are the operations reached by a second word: `mcpx task get`.
	Verbs []opVerb
}

type opVerb struct {
	Word string
	Op   api.Op
}

// verbAliases are the spellings a shell user reaches for before the one the
// table uses. Resolved only when the typed word is not itself a verb.
var verbAliases = map[string]string{"ls": "list", "rm": "delete", "show": "get"}

// opGroups derives the generated commands from the ops table.
func opGroups() []opGroup {
	hand := (&App{}).handWritten()
	byName := map[string]*opGroup{}
	for _, op := range api.Ops() {
		if !op.GeneratedCommand() {
			continue
		}
		words := op.CLIWords()
		g := byName[words[0]]
		if g == nil {
			g = &opGroup{Name: words[0]}
			byName[words[0]] = g
		}
		if len(words) == 1 {
			o := op
			g.Bare = &o
			continue
		}
		g.Verbs = append(g.Verbs, opVerb{Word: strings.Join(words[1:], "-"), Op: op})
	}
	out := make([]opGroup, 0, len(byName))
	for _, g := range byName {
		for _, op := range g.ops() {
			seg := strings.SplitN(op.Name, "_", 2)[0]
			if seg == g.Name || byName[seg] != nil || hand[seg] != nil || contains(g.Aliases, seg) {
				continue
			}
			g.Aliases = append(g.Aliases, seg)
		}
		sort.Strings(g.Aliases)
		// The listing first, since it is what a bare noun runs and what
		// anyone reaches for before they know an id.
		sort.Slice(g.Verbs, func(i, j int) bool {
			if li, lj := g.Verbs[i].Word == "list", g.Verbs[j].Word == "list"; li != lj {
				return li
			}
			return g.Verbs[i].Word < g.Verbs[j].Word
		})
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (g opGroup) ops() []api.Op {
	var out []api.Op
	if g.Bare != nil {
		out = append(out, *g.Bare)
	}
	for _, v := range g.Verbs {
		out = append(out, v.Op)
	}
	return out
}

func (g opGroup) verb(word string) (opVerb, bool) {
	for _, try := range []string{word, verbAliases[word]} {
		for _, v := range g.Verbs {
			if try != "" && v.Word == try {
				return v, true
			}
		}
	}
	return opVerb{}, false
}

func (g opGroup) verbWords() []string {
	out := make([]string, 0, len(g.Verbs))
	for _, v := range g.Verbs {
		out = append(out, v.Word)
	}
	return out
}

// runOpGroup picks the operation a generated command's arguments name.
func (a *App) runOpGroup(ctx context.Context, g opGroup, args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if v, ok := g.verb(args[0]); ok {
			return a.runOp(ctx, g.Name+" "+v.Word, v.Op, args[1:])
		}
		if g.Bare == nil {
			return fmt.Errorf("%s has no %q; it takes %s",
				g.Name, args[0], strings.Join(g.verbWords(), ", "))
		}
	}
	if g.Bare != nil {
		return a.runOp(ctx, g.Name, *g.Bare, args)
	}
	// A family with a listing lists when named alone, the way `mcpx elicit`
	// and `mcpx recipes` already do: the first thing anyone wants from a
	// noun is to see what there is.
	if v, ok := g.verb("list"); ok {
		return a.runOp(ctx, g.Name+" "+v.Word, v.Op, args)
	}
	_ = a.helpFor(g.Name)
	return fmt.Errorf("%s needs one of: %s", g.Name, strings.Join(g.verbWords(), ", "))
}

// paramFlag is the flag spelling of a parameter name: skipDefault becomes
// --skip-default, the way every other flag mcpx has is spelled.
func paramFlag(name string) string {
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

// isSwitch reports whether a parameter reads as a switch on the command
// line. The discovery routes spell their booleans as "0"/"1" query values;
// making a person type `--skip-default 1` would be the wire format leaking.
func isSwitch(p api.Param) bool {
	if p.Type == "boolean" {
		return true
	}
	return len(p.Enum) == 2 && p.Enum[0] == "0" && p.Enum[1] == "1"
}

// positional is the order positional arguments fill parameters in: the path
// first, because `mcpx task get <id>` is how everyone expects to name the
// thing, then whatever else the operation cannot do without.
func positional(op api.Op) []api.Param {
	var out []api.Param
	for _, p := range op.Params {
		if p.In == api.InPath {
			out = append(out, p)
		}
	}
	for _, p := range op.Params {
		if p.In != api.InPath && p.Required {
			out = append(out, p)
		}
	}
	return out
}

// OpUsage is the argument shape of a generated command.
func OpUsage(op api.Op) string {
	var parts []string
	for _, p := range positional(op) {
		name := "<" + paramFlag(p.Name) + ">"
		if p.DefaultCwd {
			name = "[" + paramFlag(p.Name) + "]"
		}
		parts = append(parts, name)
	}
	for _, p := range op.Params {
		if p.In == api.InPath || p.Required {
			continue
		}
		if isSwitch(p) {
			parts = append(parts, "[--"+paramFlag(p.Name)+"]")
		} else {
			parts = append(parts, "[--"+paramFlag(p.Name)+" v]")
		}
	}
	return strings.Join(parts, " ")
}

// runOp performs one operation from command-line arguments.
//
// Every flag comes from the operation's declared parameters, so the command
// cannot accept an argument the route ignores or miss one it takes. The same
// op.Request that builds the MCP proxy's request builds this one, which is
// what makes `mcpx task get X` and mcpx_task_get({id: X}) the same call.
func (a *App) runOp(ctx context.Context, cmd string, op api.Op, args []string) error {
	fs := newFlagSet(strings.Fields(cmd)[0])
	strs := map[string]*string{}
	switches := map[string]*bool{}
	valued := map[string]bool{"o": true}
	for _, p := range op.Params {
		desc := p.Desc
		if len(p.Enum) > 0 && !isSwitch(p) {
			desc = strings.TrimSpace(desc + " (" + strings.Join(p.Enum, ", ") + ")")
		}
		if isSwitch(p) {
			switches[p.Name] = fs.Bool(paramFlag(p.Name), false, desc)
			continue
		}
		strs[p.Name] = fs.String(paramFlag(p.Name), "", desc)
		valued[paramFlag(p.Name)] = true
	}
	outFile := fs.String("o", "", "write the response body to this file instead of printing it")
	if sch, err := settings.New(settings.Registry()); err == nil {
		// Settings flags bound to this command take values too, and a value
		// mistaken for a positional argument would be a baffling error.
		for _, s := range sch.ForCommand(strings.Fields(cmd)[0]) {
			if s.Kind != settings.KindBool {
				valued[s.FlagName()] = true
				for _, alias := range s.FlagAliases {
					valued[alias] = true
				}
			}
		}
	}
	// The operation's own flags only. PrintDefaults would add every setting
	// bound to the command, a hundred lines that bury the three that matter;
	// `mcpx help <cmd>` is where those are listed.
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "mcpx %s %s\n\n  %s. %s %s on the daemon.\n\n",
			cmd, OpUsage(op), op.Summary, op.Method, op.Path)
		pos := map[string]bool{}
		for _, p := range positional(op) {
			pos[p.Name] = true
		}
		for _, p := range op.Params {
			kind := " <" + paramType(p) + ">"
			if isSwitch(p) {
				kind = ""
			}
			desc := p.Desc
			if pos[p.Name] {
				desc = strings.TrimSpace(desc + " (or give it as an argument)")
			}
			fmt.Fprintf(os.Stderr, "  --%s%s\n      %s\n", paramFlag(p.Name), kind, desc)
		}
		fmt.Fprintf(os.Stderr, "  -o <file>\n      write the response body to this file instead of printing it\n")
		// --json is claimed only where it is honoured. printOp writes a Text
		// operation's bytes as they came and returns before it reads a.JSON,
		// and a streaming operation returns from runOp without reaching
		// printOp at all -- so on those two shapes the flag parses and does
		// nothing. Advertising it there is the same defect as the -o flag
		// this block advertised and ignored: the response has no JSON form
		// on /v1 either, so the honest fix is to stop promising one.
		hint := fmt.Sprintf("`mcpx help %s` for the settings it reads.", strings.Fields(cmd)[0])
		if !op.Text && !op.Streams {
			hint = "--json for the response as JSON; " + hint
		}
		fmt.Fprintf(os.Stderr, "\n  %s\n", hint)
	}
	if err := parseFlags(a, fs, hoistOpFlags(args, valued)); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	given := map[string]bool{}
	fs.Visit(func(f *flagValue) { given[f.Name] = true })
	values := map[string]any{}
	for _, p := range op.Params {
		name := paramFlag(p.Name)
		if !given[name] {
			continue
		}
		if b, ok := switches[p.Name]; ok {
			values[p.Name] = switchValue(p, *b)
			continue
		}
		values[p.Name] = *strs[p.Name]
	}
	rest := fs.Args()
	for _, p := range positional(op) {
		if len(rest) == 0 {
			break
		}
		if _, set := values[p.Name]; set {
			continue
		}
		values[p.Name], rest = rest[0], rest[1:]
	}
	if len(rest) > 0 {
		return fmt.Errorf("%s: unexpected %q\nusage: mcpx %s %s", cmd, rest[0], cmd, OpUsage(op))
	}

	req := map[string]any{}
	for _, p := range op.Params {
		v, set := values[p.Name]
		if !set && p.DefaultCwd {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			v, set = wd, true
		}
		if !set {
			if p.Required {
				return fmt.Errorf("%s: %s is required\nusage: mcpx %s %s",
					cmd, paramFlag(p.Name), cmd, OpUsage(op))
			}
			continue
		}
		conv, err := convertParam(p, v)
		if err != nil {
			return fmt.Errorf("%s: --%s: %w", cmd, paramFlag(p.Name), err)
		}
		req[p.Name] = conv
	}
	path, body, err := op.Request(req)
	if err != nil {
		return err
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	if op.Streams {
		// -o is printed in this command's usage like any other, so it has to
		// work here too: a flag that is advertised and ignored is the bug
		// this table exists to prevent. The file is opened before the stream
		// starts, so a path that cannot be written fails immediately rather
		// than after the first event.
		w := io.Writer(os.Stdout)
		if *outFile != "" {
			f, ferr := os.OpenFile(*outFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaults.PublicMode)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			w = f
		}
		return c.streamOp(ctx, op.Method, path, body, w)
	}
	raw, err := c.DoOp(ctx, op, path, body)
	if err != nil {
		return err
	}
	return a.printOp(op, raw, *outFile)
}

// flagValue is the type flag.Visit hands back; named so the closure above
// reads as what it is.
type flagValue = flag.Flag

func paramType(p api.Param) string {
	switch {
	case p.Raw:
		return "bytes|@file|-"
	case p.Type == "object" || p.Type == "array":
		return "json|@file"
	case len(p.Enum) > 0:
		return strings.Join(p.Enum, "|")
	case p.Type == "":
		return "string"
	}
	return p.Type
}

func switchValue(p api.Param, on bool) any {
	if p.Type == "boolean" {
		return on
	}
	if on {
		return "1"
	}
	return "0"
}

// convertParam turns a command-line string into the value the operation's
// schema declares, so a bad integer is refused here with the flag's name
// rather than by the daemon with a field name the person never typed.
func convertParam(p api.Param, v any) (any, error) {
	s, isString := v.(string)
	if !isString {
		return v, nil
	}
	if p.DefaultCwd && s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			return nil, err
		}
		return abs, nil
	}
	if p.Raw {
		return readArgument(s)
	}
	switch p.Type {
	case "integer":
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not a whole number", s)
		}
		return n, nil
	case "boolean":
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, fmt.Errorf("%q is not true or false", s)
		}
		return b, nil
	case "object", "array":
		text, err := readArgument(s)
		if err != nil {
			return nil, err
		}
		var out any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			return nil, fmt.Errorf("not valid JSON: %w", err)
		}
		return out, nil
	}
	if len(p.Enum) > 0 && !contains(p.Enum, s) {
		return nil, fmt.Errorf("%q is not one of %s", s, strings.Join(p.Enum, ", "))
	}
	return s, nil
}

// readArgument is curl's convention: @path reads a file, - reads stdin, and
// anything else is the value itself.
func readArgument(s string) (string, error) {
	switch {
	case s == "-":
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	case strings.HasPrefix(s, "@"):
		b, err := os.ReadFile(s[1:])
		return string(b), err
	}
	return s, nil
}

// hoistOpFlags moves flags ahead of positional arguments, since Go's parser
// stops at the first positional and `mcpx task get tsk-1 --json` is how
// people type. A bare "-" is a positional -- it means stdin.
func hoistOpFlags(args []string, valued map[string]bool) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			rest = append(rest, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if valued[name] && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, rest...)
}

// DoOp performs a declared operation's request, sending a raw body as bytes.
func (c *Client) DoOp(ctx context.Context, op api.Op, path string, body []byte) ([]byte, error) {
	if _, raw := op.RawParam(); raw {
		return c.send(ctx, op.Method, path, body, "application/octet-stream")
	}
	return c.Do(ctx, op.Method, path, body)
}

// streamOp copies an event stream's payloads to w, one JSON document a line.
//
// The SSE framing is dropped because what a shell pipeline wants is NDJSON:
// `mcpx events | jq` should work without anybody learning what "data:" is.
// No timeout: the client's ordinary one would cut a healthy stream off, and
// the context -- Ctrl-C -- is what ends it.
func (c *Client) streamOp(ctx context.Context, method, path string, body []byte, w io.Writer) error {
	base := c.endpoint
	if base == "" {
		base = "http://mcpx"
	}
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	// The operation's own method. Every streaming op is a GET today, so this
	// is not a live bug -- but the body was already computed above and
	// discarded, which is the shape a POST stream would silently fail in.
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{Transport: c.hc.Transport}).Do(req)
	if err != nil {
		if isDialErr(err) {
			return ErrNoDaemon
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return &HTTPError{Status: resp.StatusCode, Body: b,
			Msg: fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))}
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, c.set.Bytes("http.streamBufferInit")),
		int(c.set.Bytes("http.streamBufferMax")))
	var frames int
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			frames++
			if _, err := fmt.Fprintln(w, data); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// A clean EOF is not success here. api.Op.Streams marks "an endpoint
	// whose response never ends", so the far end hanging up is the far end
	// having a problem -- and this returned nil for it, which meant the
	// command exited 0 having printed nothing and written nothing. That is
	// byte-for-byte what a healthy stream nobody has published to yet looks
	// like from the outside, so there was no way to tell a dead `mcpx
	// events` from a quiet one. A CI failure was read as the first for a day
	// when it was the second; the cost of the ambiguity is why this is an
	// error rather than a log line.
	//
	// The count is in the message because the two cases want different
	// answers: nothing at all usually means the subscription never took,
	// and some-then-stop means the daemon went away mid-stream.
	return fmt.Errorf("the event stream ended after %d event(s): the daemon closed %s "+
		"without being asked to", frames, path)
}

// printOp shows an operation's answer: the bytes to a file with -o, the
// JSON document with --json, text as text, and anything else as a table or
// a list of fields a person can read.
func (a *App) printOp(op api.Op, raw []byte, outFile string) error {
	if outFile != "" {
		if err := os.WriteFile(outFile, raw, defaults.PublicMode); err != nil {
			return err
		}
		fmt.Println(outFile)
		return nil
	}
	if op.Text {
		if _, err := os.Stdout.Write(raw); err != nil {
			return err
		}
		// Text that ends mid-line leaves the prompt on the same line; bytes
		// that are not text are left exactly as they came.
		if len(raw) > 0 && raw[len(raw)-1] != '\n' && utf8.Valid(raw) {
			fmt.Println()
		}
		return nil
	}
	var doc any
	if len(raw) == 0 || json.Unmarshal(raw, &doc) != nil {
		_, err := os.Stdout.Write(raw)
		return err
	}
	if a.JSON {
		return a.out(doc)
	}
	renderHuman(os.Stdout, doc)
	return nil
}

// renderHuman prints a JSON document for a person.
//
// Generic on purpose: the operations it serves are the ones nobody wrote a
// formatter for, and a formatter written per operation is exactly the
// hand-maintained surface the generated commands exist to remove. The one
// shape worth recognising is the listing -- an object holding one array of
// records -- which becomes a table, because that is most of what /v1 returns.
func renderHuman(w io.Writer, doc any) {
	switch v := doc.(type) {
	case map[string]any:
		if key, rows, ok := listing(v); ok {
			for _, k := range sortedFieldNames(v) {
				if k != key && isScalar(v[k]) {
					fmt.Fprintf(w, "%s: %s\n", k, scalarText(v[k]))
				}
			}
			if len(rows) == 0 {
				fmt.Fprintf(w, "No %s.\n", key)
				return
			}
			renderTable(w, rows)
			return
		}
		renderFields(w, v, "")
	case []any:
		if rows, ok := records(v); ok {
			renderTable(w, rows)
			return
		}
		for _, e := range v {
			fmt.Fprintln(w, scalarText(e))
		}
	default:
		fmt.Fprintln(w, scalarText(v))
	}
}

// listing finds the one array of records in an object, if there is exactly
// one; two would make choosing between them a guess.
func listing(m map[string]any) (string, []map[string]any, bool) {
	found := ""
	var rows []map[string]any
	for k, v := range m {
		arr, isArr := v.([]any)
		if !isArr {
			continue
		}
		recs, ok := records(arr)
		if !ok {
			continue
		}
		if found != "" {
			return "", nil, false
		}
		found, rows = k, recs
	}
	return found, rows, found != ""
}

func records(arr []any) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// renderTable prints records as columns: every field that is a scalar in
// some row, identifying fields first. A nested value is left out of the
// table rather than squeezed into a cell; --json has all of it.
func renderTable(w io.Writer, rows []map[string]any) {
	cols := map[string]bool{}
	for _, r := range rows {
		for k, v := range r {
			if isScalar(v) {
				cols[k] = true
			}
		}
	}
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	sortFieldNames(names)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	head := make([]string, len(names))
	for i, n := range names {
		head[i] = strings.ToUpper(n)
	}
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	width := defaults.OpCellWidth
	for _, r := range rows {
		cells := make([]string, len(names))
		for i, n := range names {
			if v, ok := r[n]; ok && isScalar(v) {
				cells[i] = truncate(oneLine(scalarText(v)), width)
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()
}

func renderFields(w io.Writer, m map[string]any, indent string) {
	for _, k := range sortedFieldNames(m) {
		v := m[k]
		if isScalar(v) {
			fmt.Fprintf(w, "%s%s: %s\n", indent, k, scalarText(v))
			continue
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			fmt.Fprintf(w, "%s%s:\n", indent, k)
			renderFields(w, sub, indent+"  ")
			continue
		}
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "%s%s: %s\n", indent, k, b)
	}
}

func isScalar(v any) bool {
	switch v.(type) {
	case nil, string, float64, bool:
		return true
	}
	return false
}

func scalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func sortedFieldNames(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortFieldNames(out)
	return out
}

// sortFieldNames puts the fields that say which thing a row is ahead of the
// fields that describe it, and the rest in alphabetical order.
func sortFieldNames(names []string) {
	rank := func(n string) int {
		switch n {
		case "id":
			return 0
		case "name":
			return 1
		case "namespace", "server":
			return 2
		case "status", "state":
			return 3
		}
		return 4
	}
	sort.Slice(names, func(i, j int) bool {
		if ri, rj := rank(names[i]), rank(names[j]); ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
}
