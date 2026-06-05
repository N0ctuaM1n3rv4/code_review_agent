package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code-review-agent/internal/prompt"
	"code-review-agent/internal/tools"
)

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	registry, err := tools.NewRegistry(dir, 0)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return &Agent{
		tools: registry,
		prompts: prompt.Prompts{
			Templates: map[string]string{
				"end_audit_confirmation": "需要确认：\n!{files}",
			},
		},
	}
}

func mustEndAuditArgs(t *testing.T, summary, nextSteps string) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"summary":    summary,
		"next_steps": nextSteps,
	})
	if err != nil {
		t.Fatalf("marshal end_audit args: %v", err)
	}
	return data
}

func TestEndAuditCheckBlocksPendingTodo(t *testing.T) {
	agent := newTestAgent(t)
	agent.tools.RestoreSnapshot(tools.Snapshot{
		Todos: []tools.Todo{{ID: 1, Title: "审计 main.go 入口", Status: "pending", Priority: "high"}},
		Files: []tools.FileReview{{Path: "main.go", Status: "reviewed"}},
	})
	check := agent.endAuditCheck(mustEndAuditArgs(t, "done", ""))
	if check.Allow {
		t.Fatalf("expected pending todo to block end_audit")
	}
	if check.NeedsConfirm {
		t.Fatalf("expected hard block, not confirm path")
	}
	if !strings.Contains(check.BlockingReason, "未完成 Todo") {
		t.Fatalf("unexpected blocking reason: %s", check.BlockingReason)
	}
}

func TestEndAuditCheckBlocksTrackingFlow(t *testing.T) {
	agent := newTestAgent(t)
	agent.tools.RestoreSnapshot(tools.Snapshot{
		Files: []tools.FileReview{{Path: "main.go", Status: "reviewed"}},
		Flows: []tools.FlowReview{{Name: "入口到 sink", Status: "tracking", NextStep: "继续读 parser.go"}},
	})
	check := agent.endAuditCheck(mustEndAuditArgs(t, "done", ""))
	if check.Allow {
		t.Fatalf("expected tracking flow to block end_audit")
	}
	if !strings.Contains(check.BlockingReason, "未闭合 Flow") {
		t.Fatalf("unexpected blocking reason: %s", check.BlockingReason)
	}
}

func TestEndAuditCheckRequiresSecondConfirmationForFilesOnly(t *testing.T) {
	agent := newTestAgent(t)
	agent.tools.RestoreSnapshot(tools.Snapshot{
		Todos: []tools.Todo{{ID: 1, Title: "背景已完成", Status: "completed", Priority: "high", AutomationTaskID: 1}},
		Tasks: []tools.Task{{ID: 1, Type: "repo_context", Title: "背景已完成", Priority: "high", Status: "done", TodoID: 1}},
		Files: []tools.FileReview{
			{Path: "main.go", Status: "reviewed"},
			{Path: "README.md", Status: "unseen"},
		},
	})
	first := agent.endAuditCheck(mustEndAuditArgs(t, "done", ""))
	if first.Allow || !first.NeedsConfirm {
		t.Fatalf("expected first end_audit to require confirmation, got %+v", first)
	}
	second := agent.endAuditCheck(mustEndAuditArgs(t, "剩余文件是文档", ""))
	if !second.Allow {
		t.Fatalf("expected second end_audit to pass when only files remain, got %+v", second)
	}
}

func TestEndAuditCheckRejectsConflictingNextSteps(t *testing.T) {
	agent := newTestAgent(t)
	agent.tools.RestoreSnapshot(tools.Snapshot{
		Files: []tools.FileReview{{Path: "main.go", Status: "reviewed"}},
	})
	check := agent.endAuditCheck(mustEndAuditArgs(t, "done", "继续审计 auth.c 和 acl.c"))
	if check.Allow {
		t.Fatalf("expected conflicting next_steps to block end_audit")
	}
	if !strings.Contains(check.BlockingReason, "next_steps 与结束状态冲突") {
		t.Fatalf("unexpected blocking reason: %s", check.BlockingReason)
	}
}
