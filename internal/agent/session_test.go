package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"code-review-agent/internal/prompt"
	"code-review-agent/internal/tools"
)

func TestLoadSessionAllowsNilTrajectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	registry, err := tools.NewRegistry(dir, 0)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	agent := &Agent{tools: registry, prompts: prompt.Prompts{}}
	sessionPath := filepath.Join(dir, "session.json")
	payload := Session{
		Workspace: dir,
		Snapshot: tools.Snapshot{
			Files: []tools.FileReview{{Path: "main.go", Status: "reviewed"}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := os.WriteFile(sessionPath, data, 0600); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := agent.LoadSession(sessionPath); err != nil {
		t.Fatalf("load session: %v", err)
	}
	if agent.trajectory != nil {
		t.Fatalf("expected nil trajectory to remain nil")
	}
}
