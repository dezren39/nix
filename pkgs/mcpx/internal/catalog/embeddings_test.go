package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSubwordEmbedder(t *testing.T) {
	emb := &SubwordEmbedder{}
	vec, err := emb.Embed(context.Background(), "click the submit button")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != VectorDim {
		t.Fatalf("expected dim %d, got %d", VectorDim, len(vec))
	}
	if emb.Backend() != "local" {
		t.Errorf("expected backend 'local', got %s", emb.Backend())
	}
}

func TestRemoteEmbedderMock(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"data": []map[string]any{
				{
					"embedding": []float32{0.5, 0.5, 0.5, 0.5},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	rem := NewRemoteEmbedder(mockServer.URL, "test-key", "test-model")
	vec, err := rem.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 4 {
		t.Fatalf("expected length 4, got %d", len(vec))
	}
	sim := vec.CosineSimilarity(vec)
	if sim < 0.99 || sim > 1.01 {
		t.Errorf("expected unit norm similarity ~1.0, got %f", sim)
	}
}

func TestCascadeEmbedderFallback(t *testing.T) {
	// Remote server that returns 500
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failingServer.Close()

	rem := NewRemoteEmbedder(failingServer.URL, "key", "model")
	casc := NewCascadeEmbedder("auto", rem, nil)

	// Should fallback to local subword engine
	vec, err := casc.Embed(context.Background(), "git commit")
	if err != nil {
		t.Fatalf("expected successful fallback, got error: %v", err)
	}
	if len(vec) != VectorDim {
		t.Fatalf("expected dim %d, got %d", VectorDim, len(vec))
	}
}

func TestVectorIndexExportAndImport(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath1 := filepath.Join(tmpDir, "idx1.db")
	idx1, err := NewVectorIndex(dbPath1)
	if err != nil {
		t.Fatal(err)
	}
	defer idx1.Close()

	idx1.IndexTool("demo.echo", "echo a message back")
	idx1.IndexTool("demo.add", "add two numbers together")

	var buf bytes.Buffer
	if err := idx1.Export(&buf); err != nil {
		t.Fatal(err)
	}

	dbPath2 := filepath.Join(tmpDir, "idx2.db")
	idx2, err := NewVectorIndex(dbPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()

	count, err := idx2.Import(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected 2 imported tools, got %d", count)
	}

	v1, ok1 := idx1.Get("demo.echo")
	v2, ok2 := idx2.Get("demo.echo")
	if !ok1 || !ok2 {
		t.Fatalf("missing vector in idx1 or idx2")
	}
	sim := v1.CosineSimilarity(v2)
	if sim < 0.999 {
		t.Errorf("imported vector does not match original, sim=%f", sim)
	}
}
