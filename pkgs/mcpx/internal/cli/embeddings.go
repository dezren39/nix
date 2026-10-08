package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/dezren39/mcpx/internal/catalog"
)

// CmdEmbeddings implements the `mcpx embeddings` CLI commands.
func (a *App) CmdEmbeddings(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mcpx embeddings <test|export|import> [args...]")
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "test":
		if len(rest) == 0 {
			return fmt.Errorf("usage: mcpx embeddings test <text>")
		}
		text := strings.Join(rest, " ")
		c := a.Client()
		if err := c.EnsureDaemon(ctx); err == nil {
			// Query via daemon /v1/embeddings
			ep, err := c.Endpoint(ctx)
			if err == nil {
				reqBody, _ := json.Marshal(map[string]any{
					"input": text,
				})
				httpReq, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ep, "/")+"/v1/embeddings", bytes.NewReader(reqBody))
				if err == nil {
					httpReq.Header.Set("Content-Type", "application/json")
					resp, err := http.DefaultClient.Do(httpReq)
					if err == nil && resp.StatusCode == http.StatusOK {
						defer resp.Body.Close()
						var out struct {
							Model string `json:"model"`
							Data  []struct {
								Embedding []float32 `json:"embedding"`
							} `json:"data"`
						}
						if err := json.NewDecoder(resp.Body).Decode(&out); err == nil && len(out.Data) > 0 {
							vec := out.Data[0].Embedding
							fmt.Printf("Backend Model: %s\n", out.Model)
							fmt.Printf("Vector Dimension: %d\n", len(vec))
							if len(vec) > 5 {
								fmt.Printf("Sample Values: [%.4f, %.4f, %.4f, %.4f, %.4f...]\n", vec[0], vec[1], vec[2], vec[3], vec[4])
							}
							return nil
						}
					}
				}
			}
		}

		// Fallback to local subword embedding
		vec := catalog.Embed(text)
		fmt.Printf("Backend Model: pure-go subword intent (local fallback)\n")
		fmt.Printf("Vector Dimension: %d\n", len(vec))
		if len(vec) > 5 {
			fmt.Printf("Sample Values: [%.4f, %.4f, %.4f, %.4f, %.4f...]\n", vec[0], vec[1], vec[2], vec[3], vec[4])
		}
		return nil

	case "export":
		fs := newFlagSet("embeddings export")
		outPath := fs.String("output", "", "output file path (default stdout)")
		if err := parseFlags(a, fs, rest); err != nil {
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

		httpReq, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(ep, "/")+"/v1/embeddings/export", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("export failed (status %d): %s", resp.StatusCode, string(body))
		}

		if *outPath != "" {
			f, err := os.Create(*outPath)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(f, resp.Body)
			if err == nil {
				fmt.Printf("Exported embeddings saved to %s\n", *outPath)
			}
			return err
		}

		_, err = io.Copy(os.Stdout, resp.Body)
		return err

	case "import":
		if len(rest) == 0 {
			return fmt.Errorf("usage: mcpx embeddings import <file.json>")
		}
		filePath := rest[0]
		data, err := os.ReadFile(filePath)
		if err != nil {
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

		httpReq, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ep, "/")+"/v1/embeddings/import", bytes.NewReader(data))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("import failed (status %d): %s", resp.StatusCode, string(body))
		}

		var res struct {
			Imported int `json:"imported"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return err
		}
		fmt.Printf("Successfully imported %d tool embeddings into daemon index.\n", res.Imported)
		return nil

	default:
		return fmt.Errorf("unknown subcommand %q: expected test, export, or import", sub)
	}
}
