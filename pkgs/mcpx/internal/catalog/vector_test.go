package catalog

import (
	"path/filepath"
	"testing"
)

func TestVectorCosineSimilarity(t *testing.T) {
	vecA := Embed("inspect web page contents and DOM elements")
	vecB := Embed("web browser inspection and clicking buttons")
	vecC := Embed("git commit and push branch changes to repository")

	simAB := vecA.CosineSimilarity(vecB)
	simAC := vecA.CosineSimilarity(vecC)

	if simAB <= simAC {
		t.Fatalf("expected web/browser query to be closer to web tool than git tool: simAB=%f, simAC=%f", simAB, simAC)
	}
}

func TestVectorIndexSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vectors.db")
	idx, err := NewVectorIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to create vector index: %v", err)
	}

	idx.IndexTool("chrome.snapshot", "inspect web page contents and DOM elements")
	idx.IndexTool("git.commit", "git commit staged changes to branch")
	idx.Close()

	// Reopen
	idx2, err := NewVectorIndex(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen vector index: %v", err)
	}
	defer idx2.Close()

	vec, ok := idx2.Get("chrome.snapshot")
	if !ok || len(vec) != VectorDim {
		t.Fatalf("failed to restore indexed vector from sqlite")
	}

	queryVec := Embed("inspect web page contents")
	sim := queryVec.CosineSimilarity(vec)
	if sim < 0.5 {
		t.Fatalf("expected high similarity, got %f", sim)
	}
}
