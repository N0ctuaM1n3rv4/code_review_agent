package tools

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	automationStageProjectScan     = "project_scan"
	automationStageTaskExecute     = "task_execute"
	automationStageVerification    = "verification"
	automationStageCompletionCheck = "completion_check"

	automationExplorationBudget  = 8
	automationNoProgressBudget   = 3
	automationExactRepeatBudget  = 3
	automationFamilyRepeatBudget = 4

	taskTypeRepoContext        = "repo_context"
	taskTypeBuildConfigAudit   = "build_config_audit"
	taskTypeEntryAudit         = "entry_audit"
	taskTypeInputBoundaryAudit = "input_boundary_audit"
	taskTypeQueueWorkerAudit   = "queue_or_worker_audit"
	taskTypeSinkAudit          = "sink_audit"
	taskTypeFindingVerify      = "finding_verification"
	taskTypeManualFollowup     = "manual_followup"

	taskStatusPending    = "pending"
	taskStatusInProgress = "in_progress"
	taskStatusBlocked    = "blocked"
	taskStatusVerified   = "verified"
	taskStatusBenign     = "benign"
	taskStatusDone       = "done"
)

type Task struct {
	ID                  int       `json:"id"`
	Type                string    `json:"type"`
	Title               string    `json:"title"`
	Priority            string    `json:"priority"`
	Status              string    `json:"status"`
	Targets             []string  `json:"targets,omitempty"`
	TodoID              int       `json:"todo_id,omitempty"`
	ParentTaskID        int       `json:"parent_task_id,omitempty"`
	EvidenceRefs        []string  `json:"evidence_refs,omitempty"`
	RetryCount          int       `json:"retry_count,omitempty"`
	ExplorationCount    int       `json:"exploration_count,omitempty"`
	NoProgressCount     int       `json:"no_progress_count,omitempty"`
	LastProgressAt      time.Time `json:"last_progress_at,omitempty"`
	LastToolFingerprint string    `json:"last_tool_fingerprint,omitempty"`
	LastToolRepeatCount int       `json:"last_tool_repeat_count,omitempty"`
	LastToolFamily      string    `json:"last_tool_family,omitempty"`
	LastToolFamilyCount int       `json:"last_tool_family_count,omitempty"`
	LastDispatchReason  string    `json:"last_dispatch_reason,omitempty"`
	BlockedReason       string    `json:"blocked_reason,omitempty"`
}

type AutomationState struct {
	Stage              string `json:"stage"`
	PrimaryLanguage    string `json:"primary_language,omitempty"`
	ProjectKind        string `json:"project_kind,omitempty"`
	CurrentTaskID      int    `json:"current_task_id,omitempty"`
	CurrentTaskTitle   string `json:"current_task_title,omitempty"`
	CurrentTaskType    string `json:"current_task_type,omitempty"`
	CurrentTaskStatus  string `json:"current_task_status,omitempty"`
	LastDispatchReason string `json:"last_dispatch_reason,omitempty"`
}

type repositoryProfile struct {
	PrimaryLanguage string
	ProjectKind     string
	ContextTargets  []string
	BuildTargets    []string
	EntryTargets    []string
	InputTargets    []string
	QueueTargets    []string
	SinkTargets     []string
}

func (r *Registry) initializeAutomation() {
	profile := detectRepositoryProfile(r.files)
	r.automation = AutomationState{
		Stage:           automationStageProjectScan,
		PrimaryLanguage: profile.PrimaryLanguage,
		ProjectKind:     profile.ProjectKind,
	}
	r.tasks = nil
	r.nextTaskID = 1
	for _, seed := range buildSeedTasks(profile) {
		r.createAutomationTask(seed.Type, seed.Title, seed.Priority, seed.Targets, 0, true)
	}
	r.automation.Stage = automationStageTaskExecute
	r.automation.LastDispatchReason = "系统已完成项目扫描、文件分类、入口发现和任务播种"
	r.prepareAutomationTurn()
}

type automationTaskSeed struct {
	Type     string
	Title    string
	Priority string
	Targets  []string
}

func buildSeedTasks(profile repositoryProfile) []automationTaskSeed {
	var seeds []automationTaskSeed
	add := func(taskType, title, priority string, targets []string) {
		targets = uniqueNonEmptyStrings(targets, 8)
		if len(targets) == 0 {
			return
		}
		seeds = append(seeds, automationTaskSeed{
			Type:     taskType,
			Title:    title,
			Priority: priority,
			Targets:  targets,
		})
	}
	add(taskTypeRepoContext, "了解项目背景与信任边界", "high", profile.ContextTargets)
	add(taskTypeBuildConfigAudit, "审计构建配置和安全编译选项", "high", profile.BuildTargets)
	add(taskTypeEntryAudit, "审计主入口和网络接入边界", "high", profile.EntryTargets)
	add(taskTypeInputBoundaryAudit, "审计输入缓冲区与协议解析边界", "high", profile.InputTargets)
	add(taskTypeQueueWorkerAudit, "审计队列、子进程与工作线程入口", "medium", profile.QueueTargets)
	add(taskTypeSinkAudit, "审计高风险 sink 与外部交互模块", "medium", profile.SinkTargets)
	return seeds
}

func detectRepositoryProfile(files []FileReview) repositoryProfile {
	profile := repositoryProfile{PrimaryLanguage: "generic", ProjectKind: "generic"}
	langCounts := map[string]int{}
	for _, file := range files {
		ext := strings.ToLower(path.Ext(file.Path))
		switch ext {
		case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp":
			langCounts["c_cpp"]++
		case ".go":
			langCounts["go"]++
		case ".java":
			langCounts["java"]++
		case ".py":
			langCounts["python"]++
		}
	}
	switch {
	case langCounts["c_cpp"] > 0:
		profile.PrimaryLanguage = "c_cpp"
		profile.ProjectKind = "native_service"
	case langCounts["go"] > 0:
		profile.PrimaryLanguage = "go"
		profile.ProjectKind = "service"
	case langCounts["java"] > 0:
		profile.PrimaryLanguage = "java"
		profile.ProjectKind = "service"
	case langCounts["python"] > 0:
		profile.PrimaryLanguage = "python"
		profile.ProjectKind = "script"
	}
	var context, build, entry, input, queue, sink []string
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		base := strings.ToLower(path.Base(file.Path))
		switch {
		case strings.Contains(lower, "readme"), strings.Contains(lower, "/about"), strings.Contains(lower, "/docs/"):
			context = append(context, file.Path)
		case base == "makefile", strings.Contains(lower, "configure"), strings.Contains(lower, "cmakelists.txt"), strings.Contains(lower, "config"):
			build = append(build, file.Path)
		}
		switch {
		case containsAny(lower, "daemon", "server", "listener", "smtp", "socket", "http", "main.c", "main.cc", "main.cpp"):
			entry = append(entry, file.Path)
		}
		switch {
		case containsAny(lower, "smtp", "parser", "protocol", "input", "recv", "read", "buffer"):
			input = append(input, file.Path)
		}
		switch {
		case containsAny(lower, "queue", "worker", "child", "fork", "spawn", "thread"):
			queue = append(queue, file.Path)
		}
		switch {
		case containsAny(lower, "auth", "lookup", "router", "transport", "exec", "command", "pipe", "acl"):
			sink = append(sink, file.Path)
		}
	}
	profile.ContextTargets = context
	profile.BuildTargets = build
	profile.EntryTargets = entry
	profile.InputTargets = input
	profile.QueueTargets = queue
	profile.SinkTargets = sink
	return profile
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func uniqueNonEmptyStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (r *Registry) createAutomationTask(taskType, title, priority string, targets []string, parentTaskID int, seeded bool) *Task {
	for i := range r.tasks {
		if normalizeTaskTitle(r.tasks[i].Title) == normalizeTaskTitle(title) {
			if len(targets) > 0 {
				r.tasks[i].Targets = uniqueNonEmptyStrings(append(r.tasks[i].Targets, targets...), 12)
			}
			return &r.tasks[i]
		}
	}
	if priority == "" {
		priority = "medium"
	}
	task := Task{
		ID:           r.nextTaskID,
		Type:         taskType,
		Title:        title,
		Priority:     priority,
		Status:       taskStatusPending,
		Targets:      uniqueNonEmptyStrings(targets, 12),
		ParentTaskID: parentTaskID,
	}
	r.nextTaskID++
	r.tasks = append(r.tasks, task)
	taskPtr := &r.tasks[len(r.tasks)-1]
	if seeded {
		todo := r.createSeedTodo(taskPtr)
		taskPtr.TodoID = todo.ID
	}
	return taskPtr
}

func normalizeTaskTitle(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
}

func (r *Registry) createSeedTodo(task *Task) Todo {
	for i := range r.todos {
		if r.todos[i].AutomationTaskID == task.ID || normalizeTaskTitle(r.todos[i].Title) == normalizeTaskTitle(task.Title) {
			if r.todos[i].AutomationTaskID == 0 {
				r.todos[i].AutomationTaskID = task.ID
			}
			return r.todos[i]
		}
	}
	todo := Todo{
		ID:               r.nextTodoID,
		Title:            task.Title + formatTaskTargetsSuffix(task.Targets),
		Status:           "pending",
		Priority:         task.Priority,
		AutomationTaskID: task.ID,
	}
	r.nextTodoID++
	r.todos = append(r.todos, todo)
	return todo
}

func formatTaskTargetsSuffix(targets []string) string {
	targets = uniqueNonEmptyStrings(targets, 3)
	if len(targets) == 0 {
		return ""
	}
	return "：" + strings.Join(targets, "、")
}

func (r *Registry) syncAutomationAfterMutation() {
	r.syncTasksFromTodos()
	r.syncTasksFromFlows()
	r.prepareAutomationTurn()
}

func (r *Registry) syncTasksFromTodos() {
	for i := range r.tasks {
		if r.tasks[i].TodoID == 0 {
			continue
		}
		todo := r.findTodoByID(r.tasks[i].TodoID)
		if todo == nil {
			continue
		}
		if r.tasks[i].Priority != todo.Priority && todo.Priority != "" {
			r.tasks[i].Priority = todo.Priority
		}
		switch strings.ToLower(strings.TrimSpace(todo.Status)) {
		case "completed", "done":
			if r.tasks[i].Status != taskStatusVerified && r.tasks[i].Status != taskStatusBenign {
				r.tasks[i].Status = taskStatusDone
			}
		case "pending", "reviewing", "tracking", "todo", "open", "":
			if r.tasks[i].Type == taskTypeManualFollowup && r.tasks[i].TodoID == todo.ID {
				if r.tasks[i].Status == taskStatusDone || r.tasks[i].Status == taskStatusBlocked {
					r.tasks[i].Status = taskStatusPending
					r.tasks[i].RetryCount = 0
					r.tasks[i].ExplorationCount = 0
					r.tasks[i].NoProgressCount = 0
					r.tasks[i].BlockedReason = ""
				}
			}
			if r.tasks[i].Status == "" {
				r.tasks[i].Status = taskStatusPending
			}
		}
	}
}

func (r *Registry) findTodoByID(id int) *Todo {
	for i := range r.todos {
		if r.todos[i].ID == id {
			return &r.todos[i]
		}
	}
	return nil
}

func (r *Registry) syncTasksFromFlows() {
	openFlows := map[string]FlowReview{}
	for _, flow := range r.flows {
		if !isOpenFlowStatus(flow.Status) {
			continue
		}
		name := strings.TrimSpace(flow.Name)
		if name == "" {
			continue
		}
		openFlows[name] = flow
		task := r.createAutomationTask(taskTypeManualFollowup, flowFollowupTaskTitle(name), "high", flow.Files, 0, false)
		task.Targets = uniqueNonEmptyStrings(append(task.Targets, flow.Files...), 12)
		if isTerminalTaskStatus(task.Status) || task.Status == taskStatusBlocked {
			task.Status = taskStatusPending
			task.RetryCount = 0
			task.ExplorationCount = 0
			task.NoProgressCount = 0
			task.BlockedReason = ""
		}
		task.Priority = "high"
		task.LastDispatchReason = flowFollowupDispatchReason(flow)
	}
	for i := range r.tasks {
		if !isFlowFollowupTask(r.tasks[i]) {
			continue
		}
		flowName := strings.TrimSpace(strings.TrimPrefix(r.tasks[i].Title, "闭合 Flow："))
		if _, ok := openFlows[flowName]; ok {
			continue
		}
		if !isTerminalTaskStatus(r.tasks[i].Status) {
			r.tasks[i].Status = taskStatusDone
			r.tasks[i].BlockedReason = ""
			r.tasks[i].LastDispatchReason = "对应 Flow 已闭合，自动结束跟进任务"
		}
	}
}

func isOpenFlowStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "tracking", "suspicious":
		return true
	default:
		return false
	}
}

func flowFollowupTaskTitle(name string) string {
	return "闭合 Flow：" + strings.TrimSpace(name)
}

func isFlowFollowupTask(task Task) bool {
	return task.Type == taskTypeManualFollowup && strings.HasPrefix(task.Title, "闭合 Flow：")
}

func flowFollowupDispatchReason(flow FlowReview) string {
	reason := "存在未闭合 Flow，优先收口调用链、变量和证据"
	if strings.TrimSpace(flow.NextStep) != "" {
		reason += "；下一步：" + strings.TrimSpace(flow.NextStep)
	}
	return reason
}

func (r *Registry) PrepareAutomationTurn() {
	r.prepareAutomationTurn()
}

func (r *Registry) prepareAutomationTurn() {
	current := r.currentTask()
	if current != nil && (isTerminalTaskStatus(current.Status) || strings.EqualFold(strings.TrimSpace(current.Status), taskStatusBlocked)) {
		current = nil
	}
	if current != nil {
		preempt := r.pickPreemptiveTask(*current)
		if preempt != nil {
			if current.Status == taskStatusInProgress {
				current.Status = taskStatusPending
			}
			current = nil
		}
	}
	if current == nil {
		next := r.pickNextTask()
		if next == nil {
			r.automation.Stage = automationStageCompletionCheck
			r.automation.CurrentTaskID = 0
			r.automation.CurrentTaskTitle = ""
			r.automation.CurrentTaskType = ""
			r.automation.CurrentTaskStatus = ""
			if r.automation.LastDispatchReason == "" {
				r.automation.LastDispatchReason = "当前没有活跃自动化任务，进入完成性检查"
			}
			return
		}
		if next.Status == taskStatusPending {
			next.Status = taskStatusInProgress
		}
		next.LastDispatchReason = dispatchReasonForTask(*next)
		r.automation.LastDispatchReason = next.LastDispatchReason
		current = next
	}
	r.automation.Stage = automationStageForTask(*current)
	r.automation.CurrentTaskID = current.ID
	r.automation.CurrentTaskTitle = current.Title
	r.automation.CurrentTaskType = current.Type
	r.automation.CurrentTaskStatus = current.Status
}

func automationStageForTask(task Task) string {
	switch task.Type {
	case taskTypeFindingVerify:
		return automationStageVerification
	default:
		return automationStageTaskExecute
	}
}

func dispatchReasonForTask(task Task) string {
	switch task.Type {
	case taskTypeRepoContext:
		return "优先建立仓库上下文与信任边界"
	case taskTypeBuildConfigAudit:
		return "构建配置和宏开关决定后续攻击面，优先排查"
	case taskTypeEntryAudit:
		return "外部入口优先，先看网络接入与主入口"
	case taskTypeInputBoundaryAudit:
		return "入口附近的输入缓冲区和协议解析边界风险最高"
	case taskTypeQueueWorkerAudit:
		return "子进程、队列和工作线程可能引入独立入口与权限边界"
	case taskTypeSinkAudit:
		return "高风险 sink 与外部交互模块优先闭合"
	case taskTypeFindingVerify:
		return "候选漏洞需要优先复核闭环"
	case taskTypeManualFollowup:
		if isFlowFollowupTask(task) {
			return "已有跨文件 Flow 未闭合，先收口链路和证据"
		}
		return "用户或上游流程留下了明确后续动作，需要先闭合"
	default:
		return "继续推进当前自动化任务"
	}
}

func isTerminalTaskStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case taskStatusDone, taskStatusVerified, taskStatusBenign:
		return true
	default:
		return false
	}
}

func (r *Registry) pickNextTask() *Task {
	if len(r.tasks) == 0 {
		return nil
	}
	indices := make([]int, 0, len(r.tasks))
	for i := range r.tasks {
		if strings.TrimSpace(r.tasks[i].Status) == "" {
			r.tasks[i].Status = taskStatusPending
		}
		if strings.EqualFold(strings.TrimSpace(r.tasks[i].Status), taskStatusBlocked) {
			continue
		}
		if isTerminalTaskStatus(r.tasks[i].Status) {
			continue
		}
		indices = append(indices, i)
	}
	if len(indices) == 0 {
		return nil
	}
	sort.Slice(indices, func(i, j int) bool {
		left := r.tasks[indices[i]]
		right := r.tasks[indices[j]]
		return taskSortLess(left, right)
	})
	return &r.tasks[indices[0]]
}

func taskSortLess(left, right Task) bool {
	if taskStatusRank(left.Status) != taskStatusRank(right.Status) {
		return taskStatusRank(left.Status) < taskStatusRank(right.Status)
	}
	if left.RetryCount != right.RetryCount {
		return left.RetryCount < right.RetryCount
	}
	if left.NoProgressCount != right.NoProgressCount {
		return left.NoProgressCount < right.NoProgressCount
	}
	if taskUrgencyRank(left) != taskUrgencyRank(right) {
		return taskUrgencyRank(left) > taskUrgencyRank(right)
	}
	if priorityRank(left.Priority) != priorityRank(right.Priority) {
		return priorityRank(left.Priority) > priorityRank(right.Priority)
	}
	return left.ID < right.ID
}

func taskUrgencyRank(task Task) int {
	switch {
	case task.Type == taskTypeFindingVerify:
		return 5
	case isFlowFollowupTask(task):
		return 4
	case task.Type == taskTypeManualFollowup:
		return 3
	case task.Type == taskTypeEntryAudit || task.Type == taskTypeInputBoundaryAudit:
		return 2
	default:
		return 1
	}
}

func taskCanBePreempted(task Task) bool {
	if task.Type == taskTypeFindingVerify {
		return false
	}
	return !isFlowFollowupTask(task)
}

func shouldPreemptTask(current, candidate Task) bool {
	if candidate.ID == current.ID || !taskCanBePreempted(current) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(candidate.Status), taskStatusBlocked) || isTerminalTaskStatus(candidate.Status) {
		return false
	}
	if candidate.Type == taskTypeFindingVerify && current.Type != taskTypeFindingVerify {
		return true
	}
	if isFlowFollowupTask(candidate) && !isFlowFollowupTask(current) && current.Type != taskTypeFindingVerify {
		return true
	}
	return false
}

func (r *Registry) pickPreemptiveTask(current Task) *Task {
	var candidate *Task
	for i := range r.tasks {
		task := &r.tasks[i]
		if !shouldPreemptTask(current, *task) {
			continue
		}
		if candidate == nil || taskUrgencyRank(*task) > taskUrgencyRank(*candidate) || (taskUrgencyRank(*task) == taskUrgencyRank(*candidate) && taskSortLess(*task, *candidate)) {
			candidate = task
		}
	}
	return candidate
}

func taskStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case taskStatusInProgress:
		return 0
	case taskStatusPending:
		return 1
	case taskStatusBlocked:
		return 2
	default:
		return 3
	}
}

func priorityRank(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func (r *Registry) currentTask() *Task {
	if r.automation.CurrentTaskID == 0 {
		return nil
	}
	for i := range r.tasks {
		if r.tasks[i].ID == r.automation.CurrentTaskID {
			return &r.tasks[i]
		}
	}
	return nil
}

func (r *Registry) RecordToolUsage(name string, raw json.RawMessage, result string) {
	current := r.currentTask()
	if current == nil {
		if name == "verify_finding" {
			current = r.ensureFindingVerificationTask(raw)
		}
		if current == nil {
			return
		}
	}
	if name == "verify_finding" {
		current = r.ensureFindingVerificationTask(raw)
	}
	success := toolCallSucceeded(result)
	fingerprint := toolFingerprint(name, raw)
	if fingerprint != "" {
		if current.LastToolFingerprint == fingerprint {
			current.LastToolRepeatCount++
		} else {
			current.LastToolFingerprint = fingerprint
			current.LastToolRepeatCount = 1
		}
	}
	family := toolFamily(name, raw)
	if family != "" {
		if current.LastToolFamily == family {
			current.LastToolFamilyCount++
		} else {
			current.LastToolFamily = family
			current.LastToolFamilyCount = 1
		}
	}
	addedEvidence := false
	for _, evidence := range evidenceRefsFromTool(name, raw) {
		if evidence == "" {
			continue
		}
		var changed bool
		current.EvidenceRefs, changed = appendUniqueLimitedChanged(current.EvidenceRefs, evidence, 16)
		addedEvidence = addedEvidence || changed
	}
	if success {
		switch name {
		case "file_review_update", "flow_review_update", "flow_review_delete", "variable_review_update", "todo_update", "verify_finding", "report_finding":
			r.markTaskMutationProgress(current, name)
		}
		if isExplorationTool(name) {
			r.markTaskExploration(current, addedEvidence)
		}
		switch name {
		case "verify_finding":
			current.Status = taskStatusInProgress
		case "report_finding":
			if current.Type == taskTypeFindingVerify {
				current.Status = taskStatusVerified
			}
		case "end_audit":
			r.automation.Stage = automationStageCompletionCheck
		}
	} else {
		current.NoProgressCount++
		current.LastDispatchReason = "工具调用失败，未形成有效进展"
		if current.NoProgressCount >= automationNoProgressBudget {
			r.escalateStalledTask(current, "连续工具失败且未形成闭环，自动切换任务")
		}
	}
	r.syncAutomationAfterMutation()
}

func (r *Registry) ensureFindingVerificationTask(raw json.RawMessage) *Task {
	var args struct {
		Title string `json:"title"`
		Path  string `json:"path"`
	}
	_ = json.Unmarshal(raw, &args)
	title := strings.TrimSpace(args.Title)
	if title == "" {
		title = "复核候选漏洞"
	}
	taskTitle := "复核候选漏洞：" + title
	task := r.createAutomationTask(taskTypeFindingVerify, taskTitle, "high", []string{args.Path}, 0, true)
	task.Status = taskStatusInProgress
	task.RetryCount = 0
	task.ExplorationCount = 0
	task.NoProgressCount = 0
	task.BlockedReason = ""
	r.automation.CurrentTaskID = task.ID
	r.automation.CurrentTaskTitle = task.Title
	r.automation.CurrentTaskType = task.Type
	r.automation.CurrentTaskStatus = task.Status
	r.automation.Stage = automationStageVerification
	r.automation.LastDispatchReason = "候选漏洞已进入自动化复核任务"
	return task
}

func toolFingerprint(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return name
	}
	switch name {
	case "read_file":
		var args readFileArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			return fmt.Sprintf("read_file|%s|%d|%d", strings.TrimSpace(args.Path), args.Offset, args.Limit)
		}
	case "search_content":
		var args searchArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return fmt.Sprintf("search_content|%s|%s|%s|%s", strings.TrimSpace(args.Root), strings.Join(args.includePatterns(), ","), args.Mode, compactAutomationText(args.Query, 64))
		}
	case "git_inspect":
		var args gitInspectArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return fmt.Sprintf("git_inspect|%s|%s|%s|%s|%s", args.Action, strings.TrimSpace(args.Path), args.Ref, args.Base, args.Head)
		}
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return name
	}
	keys := []string{"path", "query", "title", "name", "status", "root", "pattern", "offset", "action", "include"}
	var parts []string
	for _, key := range keys {
		if value, ok := args[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
	}
	return name + "|" + strings.Join(parts, "|")
}

func toolFamily(name string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return name
	}
	switch name {
	case "read_file":
		var args readFileArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			return "read_file|" + strings.TrimSpace(args.Path)
		}
	case "search_content":
		var args searchArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return fmt.Sprintf("search_content|%s|%s|%s", strings.TrimSpace(args.Root), strings.Join(args.includePatterns(), ","), compactAutomationText(args.Query, 40))
		}
	case "git_inspect":
		var args gitInspectArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return fmt.Sprintf("git_inspect|%s|%s", args.Action, strings.TrimSpace(args.Path))
		}
	case "flow_review_update", "flow_review_delete":
		var args flowReviewDeleteArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			return name + "|" + strings.TrimSpace(args.Name)
		}
	case "file_review_update":
		var args fileReviewUpdateArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			return name + "|" + strings.TrimSpace(firstNonEmpty(args.Path, args.Dir, args.Pattern, args.Suffix))
		}
	}
	return toolFingerprint(name, raw)
}

func evidenceRefsFromTool(name string, raw json.RawMessage) []string {
	switch name {
	case "read_file":
		var args readFileArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			return []string{fmt.Sprintf("read_file:%s@%d", strings.TrimSpace(args.Path), args.Offset)}
		}
	case "search_content":
		var args searchArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return []string{fmt.Sprintf("search_content:%s|%s|%s", strings.TrimSpace(args.Root), strings.Join(args.includePatterns(), ","), compactAutomationText(args.Query, 64))}
		}
	case "git_inspect":
		var args gitInspectArgs
		if err := json.Unmarshal(raw, &args); err == nil {
			args.normalize()
			return []string{fmt.Sprintf("git_inspect:%s|%s|%s", args.Action, strings.TrimSpace(args.Path), args.Ref)}
		}
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	keys := []string{"path", "query", "title", "name"}
	var refs []string
	for _, key := range keys {
		if value, ok := args[key]; ok {
			refs = append(refs, fmt.Sprintf("%s:%v", name, value))
		}
	}
	return refs
}

func toolCallSucceeded(result string) bool {
	if strings.TrimSpace(result) == "" {
		return true
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return true
	}
	return payload.OK
}

func isExplorationTool(name string) bool {
	switch name {
	case "list_files", "read_file", "search_content", "git_inspect":
		return true
	default:
		return false
	}
}

func (r *Registry) markTaskMutationProgress(task *Task, name string) {
	task.LastProgressAt = time.Now()
	task.ExplorationCount = 0
	task.NoProgressCount = 0
	task.RetryCount = 0
	task.LastToolRepeatCount = 0
	task.LastToolFamilyCount = 0
	task.BlockedReason = ""
	if task.Status == "" || task.Status == taskStatusPending {
		task.Status = taskStatusInProgress
	}
	switch name {
	case "flow_review_update", "flow_review_delete":
		task.LastDispatchReason = "已记录或闭合 Flow，继续收口相关调用链"
	case "file_review_update":
		task.LastDispatchReason = "已回写文件排查状态，继续闭合当前任务"
	case "variable_review_update":
		task.LastDispatchReason = "已回写变量排查状态，继续闭合当前任务"
	case "todo_update":
		task.LastDispatchReason = "已回写 Todo 状态，继续推进当前任务"
	case "verify_finding":
		task.LastDispatchReason = "候选漏洞已进入复核闭环"
	case "report_finding":
		task.LastDispatchReason = "漏洞已提交，继续深挖相邻入口和同类路径"
	}
}

func (r *Registry) markTaskExploration(task *Task, addedEvidence bool) {
	task.ExplorationCount++
	reason := ""
	if !addedEvidence {
		task.NoProgressCount++
		reason = "本轮探索未产生新的目标或证据索引"
	}
	if task.LastToolRepeatCount >= automationExactRepeatBudget {
		task.NoProgressCount++
		reason = "重复调用同一工具参数，自动累计空转"
	}
	if task.LastToolFamilyCount >= automationFamilyRepeatBudget {
		task.NoProgressCount++
		reason = "在同一读取/搜索家族内反复打转，自动累计空转"
	}
	if reason != "" {
		task.LastDispatchReason = reason
	}
	if task.NoProgressCount >= automationNoProgressBudget || task.ExplorationCount >= automationExplorationBudget {
		reason = "连续探索未回写任何文件、变量、Flow 或 Todo 状态，自动切换任务"
		if task.NoProgressCount >= automationNoProgressBudget {
			reason = "连续低收益探索未形成闭环，自动切换任务"
		}
		r.escalateStalledTask(task, reason)
	}
}

func (r *Registry) escalateStalledTask(task *Task, reason string) {
	task.RetryCount++
	task.ExplorationCount = 0
	task.NoProgressCount = 0
	task.LastToolRepeatCount = 0
	task.LastToolFamilyCount = 0
	if task.RetryCount >= 2 {
		task.Status = taskStatusBlocked
		task.BlockedReason = strings.Replace(reason, "切换任务", "阻断任务", 1)
		task.LastDispatchReason = task.BlockedReason
	} else {
		task.Status = taskStatusPending
		task.BlockedReason = reason
		task.LastDispatchReason = reason
	}
	if r.automation.CurrentTaskID == task.ID {
		r.automation.CurrentTaskID = 0
		r.automation.CurrentTaskTitle = ""
		r.automation.CurrentTaskType = ""
		r.automation.CurrentTaskStatus = ""
		r.automation.LastDispatchReason = task.LastDispatchReason
	}
}

func compactAutomationText(text string, limit int) string {
	text = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
	if limit > 0 && len(text) > limit {
		return text[:limit]
	}
	return text
}

func appendUniqueLimited(values []string, value string, limit int) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	if limit > 0 && len(values) > limit {
		return values[len(values)-limit:]
	}
	return values
}

func appendUniqueLimitedChanged(values []string, value string, limit int) ([]string, bool) {
	for _, existing := range values {
		if existing == value {
			return values, false
		}
	}
	values = append(values, value)
	if limit > 0 && len(values) > limit {
		values = values[len(values)-limit:]
	}
	return values, true
}

func (r *Registry) Tasks() []Task {
	out := make([]Task, len(r.tasks))
	copy(out, r.tasks)
	return out
}

func (r *Registry) Automation() AutomationState {
	return r.automation
}

func (r *Registry) AutomationPrompt(limit int) string {
	if limit <= 0 {
		limit = 8
	}
	r.prepareAutomationTurn()
	var b strings.Builder
	b.WriteString("# 自动化任务状态\n\n")
	b.WriteString("- stage: ")
	b.WriteString(r.automation.Stage)
	b.WriteString("\n")
	if r.automation.PrimaryLanguage != "" {
		b.WriteString("- primary_language: ")
		b.WriteString(r.automation.PrimaryLanguage)
		b.WriteString("\n")
	}
	if r.automation.ProjectKind != "" {
		b.WriteString("- project_kind: ")
		b.WriteString(r.automation.ProjectKind)
		b.WriteString("\n")
	}
	if r.automation.CurrentTaskID > 0 {
		b.WriteString(fmt.Sprintf("- current_task: #%d [%s/%s] %s\n", r.automation.CurrentTaskID, r.automation.CurrentTaskType, r.automation.CurrentTaskStatus, r.automation.CurrentTaskTitle))
	}
	if r.automation.LastDispatchReason != "" {
		b.WriteString("- dispatch_reason: ")
		b.WriteString(r.automation.LastDispatchReason)
		b.WriteString("\n")
	}
	b.WriteString("\n## 自动化任务队列\n")
	if len(r.tasks) == 0 {
		b.WriteString("暂无自动化任务。\n")
		return b.String()
	}
	for i, task := range r.tasks {
		if i >= limit {
			b.WriteString(fmt.Sprintf("- ...还有 %d 个任务未列出\n", len(r.tasks)-limit))
			break
		}
		b.WriteString(fmt.Sprintf("- #%d [%s/%s/%s] %s\n", task.ID, task.Type, task.Status, task.Priority, task.Title))
		if len(task.Targets) > 0 {
			b.WriteString("  targets: ")
			b.WriteString(strings.Join(uniqueNonEmptyStrings(task.Targets, 4), ", "))
			b.WriteString("\n")
		}
		if task.LastDispatchReason != "" {
			b.WriteString("  note: ")
			b.WriteString(task.LastDispatchReason)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
