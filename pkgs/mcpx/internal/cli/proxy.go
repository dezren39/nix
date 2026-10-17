package cli

import (
	"context"
	"fmt"
	"strings"
)

// CmdProxy displays or configures the transparent LLM reverse proxy.
func (a *App) CmdProxy(ctx context.Context, args []string) error {
	fs := newFlagSet("proxy")
	port := fs.Int("port", 0, "dedicated proxy port")
	upstream := fs.String("upstream", "", "upstream LLM base URL (e.g. https://api.openai.com/v1)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	c := a.Client()
	if err := c.EnsureDaemon(ctx); err != nil {
		return err
	}
	ep, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}

	proxyURL := strings.TrimRight(ep, "/") + "/v1"
	if *upstream != "" {
		_ = a.Settings().SetRuntime("proxy.upstreamUrl", *upstream)
	}

	fmt.Printf("mcpx LLM Reverse Proxy active.\n")
	fmt.Printf("OpenAI-compatible Base URL: %s\n", proxyURL)
	fmt.Printf("Chat completions endpoint: %s/chat/completions\n\n", proxyURL)
	fmt.Printf("To use with OpenAI SDK or AI coding agents:\n")
	fmt.Printf("  export OPENAI_BASE_URL=%s\n", proxyURL)
	if *port > 0 {
		fmt.Printf("Dedicated proxy port configured: %d\n", *port)
	}
	return nil
}
