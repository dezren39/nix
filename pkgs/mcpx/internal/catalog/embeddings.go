package catalog

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Embedder computes vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, text string) (Vector, error)
	Backend() string
}

// SubwordEmbedder produces pure-Go 384-dimensional dense vectors using n-grams and intent projections.
type SubwordEmbedder struct{}

func (s *SubwordEmbedder) Embed(_ context.Context, text string) (Vector, error) {
	return Embed(text), nil
}

func (s *SubwordEmbedder) Backend() string {
	return "local"
}

// RemoteEmbedder queries an OpenAI/Ollama/Neon-compatible POST /v1/embeddings endpoint.
type RemoteEmbedder struct {
	client  *http.Client
	url     string
	apiKey  string
	model   string
}

// NewRemoteEmbedder creates a remote embedding client.
func NewRemoteEmbedder(url, apiKey, model string) *RemoteEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &RemoteEmbedder{
		client: &http.Client{Timeout: defaults.EmbeddingsRemoteTimeout},
		url:    url,
		apiKey: apiKey,
		model:  model,
	}
}

func (r *RemoteEmbedder) Backend() string {
	return "remote"
}

func (r *RemoteEmbedder) IsConfigured() bool {
	return r != nil && r.url != ""
}

func (r *RemoteEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	if r.url == "" {
		return nil, fmt.Errorf("remote embeddings url not configured")
	}

	target := strings.TrimRight(r.url, "/")
	if !strings.HasSuffix(target, "/embeddings") {
		target += "/embeddings"
	}

	reqBody, err := json.Marshal(map[string]any{
		"input": text,
		"model": r.model,
	})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if r.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+r.apiKey)
	}

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("remote embeddings error %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned from remote")
	}

	raw := parsed.Data[0].Embedding
	vec := make(Vector, len(raw))
	copy(vec, raw)

	// L2 normalization
	var normSq float32
	for _, v := range vec {
		normSq += v * v
	}
	if normSq > 0 {
		invNorm := float32(1.0 / math.Sqrt(float64(normSq)))
		for i := range vec {
			vec[i] *= invNorm
		}
	}

	return vec, nil
}

// WasmEmbedder executes a neural transformer model compiled to WebAssembly via wazero.
type WasmEmbedder struct {
	mu       sync.Mutex
	wasmPath string
	rt       wazero.Runtime
	mod      wazero.CompiledModule
}

// NewWasmEmbedder creates a WebAssembly neural embedder from a .wasm model file.
func NewWasmEmbedder(ctx context.Context, wasmPath string) (*WasmEmbedder, error) {
	we := &WasmEmbedder{wasmPath: wasmPath}
	if wasmPath == "" {
		return we, nil
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}

	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	mod, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	we.rt = rt
	we.mod = mod
	return we, nil
}

func (w *WasmEmbedder) Backend() string {
	return "wasm"
}

func (w *WasmEmbedder) IsAvailable() bool {
	return w != nil && w.mod != nil
}

func (w *WasmEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	if !w.IsAvailable() {
		return nil, fmt.Errorf("wasm model not loaded")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	// Instantiate isolated execution instance per call
	inst, err := w.rt.InstantiateModule(ctx, w.mod, wazero.NewModuleConfig())
	if err != nil {
		return nil, err
	}
	defer inst.Close(ctx)

	// Invoke exported embed function if present
	embedFn := inst.ExportedFunction("embed")
	if embedFn == nil {
		// Fallback to default projection if function export differs
		return Embed(text), nil
	}

	// Memory write and invoke: write text into wasm linear memory
	textBytes := []byte(text)
	textLen := uint64(len(textBytes))
	allocFn := inst.ExportedFunction("allocate")
	var ptr uint64
	if allocFn != nil {
		res, err := allocFn.Call(ctx, textLen)
		if err == nil && len(res) > 0 {
			ptr = res[0]
		}
	}
	if !inst.Memory().Write(uint32(ptr), textBytes) {
		return nil, fmt.Errorf("failed to write into wasm memory")
	}

	res, err := embedFn.Call(ctx, ptr, textLen)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("wasm embed returned no vector pointer")
	}

	outPtr := uint32(res[0])
	buf, ok := inst.Memory().Read(outPtr, VectorDim*4)
	if !ok {
		return nil, fmt.Errorf("failed to read vector from wasm memory")
	}

	vec := make(Vector, VectorDim)
	for i := 0; i < VectorDim; i++ {
		bits := binary.LittleEndian.Uint32(buf[i*4 : (i+1)*4])
		vec[i] = math.Float32frombits(bits)
	}
	return vec, nil
}

func (w *WasmEmbedder) Close(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.rt != nil {
		return w.rt.Close(ctx)
	}
	return nil
}

// CascadeEmbedder orchestrates Cache -> Remote -> WASM -> Local Subword embeddings.
type CascadeEmbedder struct {
	backend string // "auto", "local", "remote", "wasm"
	local   *SubwordEmbedder
	remote  *RemoteEmbedder
	wasm    *WasmEmbedder
}

// NewCascadeEmbedder builds the tiered embedder with automatic fallbacks.
func NewCascadeEmbedder(backend string, remote *RemoteEmbedder, wasm *WasmEmbedder) *CascadeEmbedder {
	if backend == "" {
		backend = "auto"
	}
	return &CascadeEmbedder{
		backend: backend,
		local:   &SubwordEmbedder{},
		remote:  remote,
		wasm:    wasm,
	}
}

func (c *CascadeEmbedder) Backend() string {
	return c.backend
}

// Embed generates embeddings according to configured strategy and fallback cascade.
func (c *CascadeEmbedder) Embed(ctx context.Context, text string) (Vector, error) {
	switch c.backend {
	case "remote":
		if c.remote != nil && c.remote.IsConfigured() {
			vec, err := c.remote.Embed(ctx, text)
			if err == nil {
				return vec, nil
			}
		}
		// Fallback to local on error
		return c.local.Embed(ctx, text)

	case "wasm":
		if c.wasm != nil && c.wasm.IsAvailable() {
			vec, err := c.wasm.Embed(ctx, text)
			if err == nil {
				return vec, nil
			}
		}
		// Fallback to local on error
		return c.local.Embed(ctx, text)

	case "local":
		return c.local.Embed(ctx, text)

	case "auto":
		fallthrough
	default:
		// 1. Try remote if configured
		if c.remote != nil && c.remote.IsConfigured() {
			vec, err := c.remote.Embed(ctx, text)
			if err == nil {
				return vec, nil
			}
		}
		// 2. Try WASM if available
		if c.wasm != nil && c.wasm.IsAvailable() {
			vec, err := c.wasm.Embed(ctx, text)
			if err == nil {
				return vec, nil
			}
		}
		// 3. Fallback to built-in pure-Go subword engine
		return c.local.Embed(ctx, text)
	}
}

// ExportedEmbeddings defines the JSON schema for import and export.
type ExportedEmbeddings struct {
	Version int                  `json:"version"`
	Model   string               `json:"model"`
	Dim     int                  `json:"dim"`
	Vectors map[string][]float32 `json:"vectors"`
}

// Export serializes all vectors in VectorIndex to JSON format.
func (idx *VectorIndex) Export(w io.Writer) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	out := ExportedEmbeddings{
		Version: 1,
		Model:   "mcpx-embeddings",
		Dim:     VectorDim,
		Vectors: make(map[string][]float32, len(idx.vectors)),
	}
	for k, v := range idx.vectors {
		slice := make([]float32, len(v))
		copy(slice, v)
		out.Vectors[k] = slice
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Import deserializes and inserts vectors into VectorIndex and SQLite database.
func (idx *VectorIndex) Import(r io.Reader) (int, error) {
	var in ExportedEmbeddings
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return 0, err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	count := 0
	for k, v := range in.Vectors {
		if len(v) == 0 {
			continue
		}
		vec := make(Vector, len(v))
		copy(vec, v)
		idx.vectors[k] = vec
		count++

		if idx.db != nil {
			blob := make([]byte, len(v)*4)
			for i, val := range v {
				binary.LittleEndian.PutUint32(blob[i*4:(i+1)*4], math.Float32bits(val))
			}
			_, _ = idx.db.Exec(`
				INSERT OR REPLACE INTO tool_embeddings (key, vector)
				VALUES (?, ?)
			`, k, blob)
		}
	}
	return count, nil
}
