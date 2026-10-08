package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dezren39/mcpx/internal/catalog"
	"github.com/dezren39/mcpx/internal/defaults"
)

type embeddingRequest struct {
	Input any    `json:"input"` // string or []string
	Model string `json:"model"`
}

type embeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type embeddingResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
	Usage  embeddingUsage  `json:"usage"`
}

// handleEmbeddings implements OpenAI-compatible POST /v1/embeddings.
func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req embeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json request: %w", err))
		return
	}

	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok {
				inputs = append(inputs, str)
			}
		}
	case []string:
		inputs = v
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("input must be string or array of strings"))
		return
	}

	embedder := s.reg.Embedder()
	ctx, cancel := context.WithTimeout(r.Context(), defaults.EmbeddingsRemoteTimeout)
	defer cancel()

	var data []embeddingItem
	totalTokens := 0
	for i, text := range inputs {
		vec, err := embedder.Embed(ctx, text)
		if err != nil || len(vec) == 0 {
			vec = catalog.Embed(text)
		}
		data = append(data, embeddingItem{
			Object:    "embedding",
			Index:     i,
			Embedding: vec,
		})
		totalTokens += len(strings.Fields(text))
	}

	model := req.Model
	if model == "" {
		model = "mcpx-" + embedder.Backend()
	}

	writeJSON(w, http.StatusOK, embeddingResponse{
		Object: "list",
		Data:   data,
		Model:  model,
		Usage: embeddingUsage{
			PromptTokens: totalTokens,
			TotalTokens:  totalTokens,
		},
	})
}

// handleEmbeddingsExport exports all cached tool vectors as JSON.
func (s *Server) handleEmbeddingsExport(w http.ResponseWriter, r *http.Request) {
	vIdx := s.reg.VectorIndex()
	if vIdx == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("vector index not initialized"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := vIdx.Export(w); err != nil {
		s.logger.Printf("embeddings export error: %v", err)
	}
}

// handleEmbeddingsImport imports pre-computed vectors from JSON body into index.
func (s *Server) handleEmbeddingsImport(w http.ResponseWriter, r *http.Request) {
	vIdx := s.reg.VectorIndex()
	if vIdx == nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("vector index not initialized"))
		return
	}
	count, err := vIdx.Import(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("failed to import embeddings: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": count,
		"status":   "ok",
	})
}
