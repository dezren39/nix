package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CmdPrompts lists and renders prompts.
//
// Prompts are the half of MCP that is not tools: a server saying "here is the
// wording that works for this". A server that publishes a good one has
// encoded expertise that would otherwise be rediscovered by whoever writes
// the request, and mcpx previously reported none of them.
func (a *App) CmdPrompts(ctx context.Context, args []string) error {
	fs := newFlagSet("prompts")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}

	// A bare argument names a prompt to render, because listing then
	// rendering is the whole loop and a second command for the second half
	// is friction.
	if fs.NArg() > 0 {
		return a.renderPrompt(ctx, fs.Args())
	}

	list, err := c.Prompts(ctx, splitCSV(*nsFlag))
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(list)
	}
	if len(list) == 0 {
		fmt.Println("No prompts. Not every server publishes them; " +
			"`mcpx ls` shows what is configured.")
		return nil
	}
	for _, p := range list {
		fmt.Printf("%s.%s\n", p.Namespace, p.Name)
		if d := firstLine(p.Description); d != "" {
			fmt.Printf("    %s\n", d)
		}
		if len(p.Arguments) > 0 {
			var names []string
			for _, arg := range p.Arguments {
				if arg.Required {
					names = append(names, arg.Name)
					continue
				}
				names = append(names, arg.Name+"?")
			}
			fmt.Printf("    takes: %s\n", strings.Join(names, ", "))
		}
	}
	fmt.Printf("\n%d prompts. `mcpx prompts <ns>.<name> key=value` renders one.\n", len(list))
	return nil
}

func (a *App) renderPrompt(ctx context.Context, args []string) error {
	ns, name, ok := strings.Cut(args[0], ".")
	if !ok {
		return fmt.Errorf("name the prompt as <namespace>.<name>; `mcpx prompts` lists them")
	}
	// key=value pairs, because a prompt's arguments are almost always short
	// strings and making somebody write JSON for two of them is ceremony.
	values := map[string]string{}
	for _, kv := range args[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("arguments are key=value, got %q", kv)
		}
		values[k] = v
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	session := a.mcpSession()
	raw, err := c.GetPrompt(ctx, ns, name, values, a.callContext(session, session))
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(json.RawMessage(raw))
	}
	fmt.Println(renderPrompt(raw))
	return nil
}

// CmdResources lists and reads resources.
func (a *App) CmdResources(ctx context.Context, args []string) error {
	fs := newFlagSet("resources")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	templates := fs.Bool("templates", false, "list the templated resources instead")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}

	if fs.NArg() > 0 {
		ns, uri, ok := strings.Cut(fs.Arg(0), "/")
		if !ok {
			return fmt.Errorf("name the resource as <namespace>/<uri>")
		}
		session := a.mcpSession()
		raw, err := c.ReadResource(ctx, ns, uri, a.callContext(session, session))
		if err != nil {
			return err
		}
		if a.JSON {
			return a.out(json.RawMessage(raw))
		}
		text, _, _ := renderResource(raw)
		fmt.Println(text)
		return nil
	}

	if *templates {
		return a.printResourceTemplates(ctx, c, splitCSV(*nsFlag))
	}
	list, err := c.Resources(ctx, splitCSV(*nsFlag))
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(list)
	}
	if len(list) == 0 {
		fmt.Println("No resources. Not every server publishes them.")
		return nil
	}
	for _, r := range list {
		fmt.Printf("%s/%s\n", r.Namespace, r.URI)
		if d := firstLine(r.Description); d != "" {
			fmt.Printf("    %s\n", d)
		}
	}
	fmt.Printf("\n%d resources. `mcpx resources <ns>/<uri>` reads one.\n", len(list))
	return nil
}

// printResourceTemplates lists the parameterised resources, which are the
// half of `resources` a plain listing leaves out: a template is not a
// resource until somebody fills it in, so it has no URI to read.
func (a *App) printResourceTemplates(ctx context.Context, c *Client, ns []string) error {
	list, err := c.ResourceTemplatesIn(ctx, ns)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(list)
	}
	if len(list) == 0 {
		fmt.Println("No resource templates. Not every server publishes them.")
		return nil
	}
	for _, r := range list {
		fmt.Printf("%s/%s\n", r.Namespace, r.URI)
		if d := firstLine(r.Description); d != "" {
			fmt.Printf("    %s\n", d)
		}
	}
	fmt.Printf("\n%d resource templates.\n", len(list))
	return nil
}
