package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newAutomationRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"main.go":        "package main\nfunc main() {}\n",
		"parser.go":      "package main\nfunc parse() {}\n",
		"README.md":      "# test\n",
		"worker_queue.c": "int worker(void) { return 0; }\n",
	}
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	registry, err := NewRegistry(dir, 0)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return registry
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return data
}

func TestAutomationRepeatedExplorationDemotesTask(t *testing.T) {
	registry := newAutomationRegistry(t)
	registry.tasks = []Task{
		{ID: 1, Type: taskTypeEntryAudit, Title: "审计入口", Priority: "high", Status: taskStatusInProgress},
		{ID: 2, Type: taskTypeSinkAudit, Title: "审计 sink", Priority: "medium", Status: taskStatusPending},
	}
	registry.nextTaskID = 3
	registry.automation.CurrentTaskID = 1
	registry.automation.CurrentTaskTitle = registry.tasks[0].Title
	registry.automation.CurrentTaskType = registry.tasks[0].Type
	registry.automation.CurrentTaskStatus = registry.tasks[0].Status

	args := mustRawJSON(t, map[string]any{
		"query": "auth",
		"root":  ".",
	})
	for i := 0; i < 3; i++ {
		registry.RecordToolUsage("search_content", args, `{"ok":true,"data":[]}`)
	}

	if registry.tasks[0].Status != taskStatusPending {
		t.Fatalf("expected current task to be demoted to pending, got %s", registry.tasks[0].Status)
	}
	if registry.automation.CurrentTaskID == 1 {
		t.Fatalf("expected scheduler to switch away from stalled task 1")
	}
}

func TestAutomationSecondStallBlocksTask(t *testing.T) {
	registry := newAutomationRegistry(t)
	registry.tasks = []Task{
		{ID: 1, Type: taskTypeEntryAudit, Title: "审计入口", Priority: "high", Status: taskStatusInProgress},
	}
	registry.nextTaskID = 2
	registry.automation.CurrentTaskID = 1
	registry.automation.CurrentTaskTitle = registry.tasks[0].Title
	registry.automation.CurrentTaskType = registry.tasks[0].Type
	registry.automation.CurrentTaskStatus = registry.tasks[0].Status

	args := mustRawJSON(t, map[string]any{
		"query": "auth",
		"root":  ".",
	})
	for i := 0; i < 3; i++ {
		registry.RecordToolUsage("search_content", args, `{"ok":true,"data":[]}`)
	}

	registry.prepareAutomationTurn()
	if registry.automation.CurrentTaskID != 1 {
		t.Fatalf("expected the only task to be selected again for second attempt, got %d", registry.automation.CurrentTaskID)
	}
	for i := 0; i < 3; i++ {
		registry.RecordToolUsage("search_content", args, `{"ok":true,"data":[]}`)
	}

	if registry.tasks[0].Status != taskStatusBlocked {
		t.Fatalf("expected task to be blocked after second stall, got %s", registry.tasks[0].Status)
	}
	if registry.automation.CurrentTaskID == 1 {
		t.Fatalf("expected scheduler to leave blocked task")
	}
}

func TestAutomationFlowFollowupPreemptsBroadTask(t *testing.T) {
	registry := newAutomationRegistry(t)
	registry.tasks = []Task{
		{ID: 1, Type: taskTypeRepoContext, Title: "了解项目背景与信任边界", Priority: "high", Status: taskStatusInProgress},
	}
	registry.nextTaskID = 2
	registry.automation.CurrentTaskID = 1
	registry.automation.CurrentTaskTitle = registry.tasks[0].Title
	registry.automation.CurrentTaskType = registry.tasks[0].Type
	registry.automation.CurrentTaskStatus = registry.tasks[0].Status
	registry.flows = []FlowReview{{
		Name:     "入口到 sink",
		Status:   "tracking",
		Files:    []string{"main.go", "parser.go"},
		NextStep: "继续确认 parser.go 到执行点",
	}}

	registry.syncAutomationAfterMutation()

	if registry.automation.CurrentTaskID == 1 {
		t.Fatalf("expected flow followup to preempt repo context task")
	}
	current := registry.currentTask()
	if current == nil {
		t.Fatalf("expected a current task after sync")
	}
	if !isFlowFollowupTask(*current) {
		t.Fatalf("expected current task to be a flow followup, got %+v", *current)
	}
	if current.Status != taskStatusInProgress {
		t.Fatalf("expected flow followup to be in progress, got %s", current.Status)
	}
}

func TestAutomationFlowFollowupClosesWhenFlowDeleted(t *testing.T) {
	registry := newAutomationRegistry(t)
	registry.tasks = nil
	registry.automation = AutomationState{}
	registry.flows = []FlowReview{{
		Name:     "入口到 sink",
		Status:   "tracking",
		Files:    []string{"main.go"},
		NextStep: "继续验证",
	}}
	registry.syncAutomationAfterMutation()

	current := registry.currentTask()
	if current == nil || !isFlowFollowupTask(*current) {
		t.Fatalf("expected flow followup task to exist")
	}

	registry.flows = nil
	registry.syncAutomationAfterMutation()

	foundDone := false
	for _, task := range registry.tasks {
		if isFlowFollowupTask(task) && task.Status == taskStatusDone {
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatalf("expected flow followup task to be marked done when flow closes")
	}
}
