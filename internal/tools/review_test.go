package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	registry, err := NewRegistry(dir, 0)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return registry
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return data
}

func TestFileReviewUpdateRejectsInvalidStatus(t *testing.T) {
	registry := newTestRegistry(t)
	result := registry.fileReviewUpdate(mustJSON(t, map[string]any{
		"path":   "main.go",
		"status": "pending",
	}))
	if result.OK {
		t.Fatalf("expected invalid status to fail")
	}
	if !strings.Contains(result.Error, "invalid file review status") {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

func TestIncompleteWorkTreatsUnknownFileStatusAsBlocking(t *testing.T) {
	registry := newTestRegistry(t)
	registry.files[0].Status = "pending"
	work := registry.IncompleteWork()
	if len(work.UnknownFiles) != 1 {
		t.Fatalf("expected unknown file status to be tracked, got %d", len(work.UnknownFiles))
	}
	if len(work.BlockingFiles) != 1 {
		t.Fatalf("expected unknown file status to block end audit, got %d", len(work.BlockingFiles))
	}
}
