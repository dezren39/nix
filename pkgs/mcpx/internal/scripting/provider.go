package scripting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// CompletionRequest represents an inference request sent to an LLM provider.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Temperature  float64
}

// Provider represents any LLM service or local model able to complete or repair code.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req CompletionRequest) (string, error)
}

// ProviderConfig configures model resolution.
type ProviderConfig struct {
	Type     string `json:"type"`     // "openai", "ollama", "qwen", "claude", "codex", "opencode", "agy", "command"
	BaseURL  string `json:"baseUrl"`  // e.g. "http://localhost:11434/v1", "https://api.openai.com/v1"
	APIKey   string `json:"apiKey"`   // API key if required
	Model    string `json:"model"`    // Model name, e.g. "qwen2.5-coder", "gpt-4o"
	Command  string `json:"command"`  // CLI command if Type == "command"
	Timeout  time.Duration
}

// OpenAICompatibleProvider handles any OpenAI-compatible completions API
// (Ollama, local Qwen, vLLM, LM Studio, OpenAI, Claude via proxy).
type OpenAICompatibleProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAICompatibleProvider creates an OpenAI-compatible provider.
func NewOpenAICompatibleProvider(name, baseURL, apiKey, model string, timeout time.Duration) *OpenAICompatibleProvider {
	if timeout <= 0 {
		timeout = defaults.CallTimeout
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") && !strings.Contains(baseURL, "/chat/completions") {
		baseURL += "/v1"
	}
	return &OpenAICompatibleProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}
}

func (p *OpenAICompatibleProvider) Name() string { return p.name }

func (p *OpenAICompatibleProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	url := p.baseURL
	if !strings.HasSuffix(url, "/chat/completions") {
		url += "/chat/completions"
	}

	model := p.model
	if model == "" {
		model = "default"
	}

	messages := []map[string]string{}
	if req.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.UserPrompt})

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": req.Temperature,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("provider %s request failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider %s returned HTTP %d: %s", p.name, resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("provider %s invalid json response: %w", p.name, err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("provider %s returned empty choices", p.name)
	}

	return StripFences(parsed.Choices[0].Message.Content), nil
}

// CommandProvider executes an external CLI like `opencode`, `agy`, `claude`, or `codex`.
type CommandProvider struct {
	name    string
	command string
	args    []string
}

// NewCommandProvider creates a CLI provider.
func NewCommandProvider(name, cmdStr string) *CommandProvider {
	parts := strings.Fields(cmdStr)
	cmdName := parts[0]
	var args []string
	if len(parts) > 1 {
		args = parts[1:]
	}
	return &CommandProvider{
		name:    name,
		command: cmdName,
		args:    args,
	}
}

func (p *CommandProvider) Name() string { return p.name }

func (p *CommandProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	cmd := exec.CommandContext(ctx, p.command, p.args...)
	var fullPrompt strings.Builder
	if req.SystemPrompt != "" {
		fullPrompt.WriteString("System: " + req.SystemPrompt + "\n\n")
	}
	fullPrompt.WriteString(req.UserPrompt)

	cmd.Stdin = strings.NewReader(fullPrompt.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command provider %s failed (%v): %s", p.name, err, string(out))
	}
	return StripFences(string(out)), nil
}

// CallbackProvider delegates to a custom function (e.g. MCP broker sampling).
type CallbackProvider struct {
	name string
	fn   func(ctx context.Context, req CompletionRequest) (string, error)
}

func NewCallbackProvider(name string, fn func(ctx context.Context, req CompletionRequest) (string, error)) *CallbackProvider {
	return &CallbackProvider{name: name, fn: fn}
}

func (p *CallbackProvider) Name() string { return p.name }

func (p *CallbackProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	s, err := p.fn(ctx, req)
	if err != nil {
		return "", err
	}
	return StripFences(s), nil
}

// ResolveProvider inspects the environment and config to return the best available provider.
func ResolveProvider(cfg ProviderConfig, fallbackFn func(ctx context.Context, req CompletionRequest) (string, error)) Provider {
	// 1. Explicit configuration
	if cfg.BaseURL != "" {
		name := cfg.Type
		if name == "" {
			name = "http-provider"
		}
		return NewOpenAICompatibleProvider(name, cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Timeout)
	}
	if cfg.Command != "" {
		name := cfg.Type
		if name == "" {
			name = "cli-provider"
		}
		return NewCommandProvider(name, cfg.Command)
	}

	// 2. Environment variables (Local Qwen / Ollama / OpenAI / etc.)
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		return NewOpenAICompatibleProvider("openai-base-env", url, os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL"), defaults.CallTimeout)
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return NewOpenAICompatibleProvider("openai", "https://api.openai.com/v1", key, "gpt-4o-mini", defaults.CallTimeout)
	}

	// 3. Fallback callback (e.g. MCP broker sampling)
	if fallbackFn != nil {
		return NewCallbackProvider("sampling-broker", fallbackFn)
	}

	return nil
}

// StripFences removes markdown code fences (` ```ts ... ``` `) and trims whitespace.
func StripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
		lines = lines[1:]
	}
	if len(lines) >= 1 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
