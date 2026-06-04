package trajectory

import (
	"encoding/json"
	"time"
)

type Action struct {
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Step struct {
	Step        int       `json:"step"`
	Thought     string    `json:"thought"`
	Action      Action    `json:"action"`
	Observation string    `json:"observation"`
	At          time.Time `json:"at"`
	Latency     string    `json:"latency,omitempty"`
	TokenCount  int       `json:"token_count,omitempty"`
	IsRetry     bool      `json:"is_retry,omitempty"`
	FromSummary bool      `json:"from_summary,omitempty"`
	SubAgentOf  string    `json:"sub_agent_of,omitempty"`
}

type Trajectory struct {
	ID          string    `json:"id"`
	Instruction string    `json:"instruction"`
	Model       string    `json:"model"`
	Workspace   string    `json:"workspace"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at,omitempty"`
	Steps       []Step    `json:"steps"`
}

func New(id, instruction, model, workspace string) *Trajectory {
	return &Trajectory{
		ID:          id,
		Instruction: instruction,
		Model:       model,
		Workspace:   workspace,
		StartedAt:   time.Now(),
	}
}

func (t *Trajectory) AddStep(thought string, actionType string, actionParams json.RawMessage, observation string, opts ...StepOption) Step {
	step := Step{
		Step:        len(t.Steps) + 1,
		Thought:     thought,
		Action:      Action{Type: actionType, Params: actionParams},
		Observation: observation,
		At:          time.Now(),
	}
	for _, opt := range opts {
		opt(&step)
	}
	t.Steps = append(t.Steps, step)
	return step
}

func (t *Trajectory) AddRetryStep(thought string, actionType string, actionParams json.RawMessage, observation string) Step {
	return t.AddStep(thought, actionType, actionParams, observation, WithIsRetry(true))
}

func (t *Trajectory) End() {
	t.EndedAt = time.Now()
}

func (t *Trajectory) LastStep() *Step {
	if len(t.Steps) == 0 {
		return nil
	}
	return &t.Steps[len(t.Steps)-1]
}

type StepOption func(*Step)

func WithLatency(latency string) StepOption {
	return func(s *Step) { s.Latency = latency }
}

func WithTokenCount(count int) StepOption {
	return func(s *Step) { s.TokenCount = count }
}

func WithSubAgentOf(parentID string) StepOption {
	return func(s *Step) { s.SubAgentOf = parentID }
}

func WithAt(t time.Time) StepOption {
	return func(s *Step) { s.At = t }
}

func WithIsRetry(val bool) StepOption {
	return func(s *Step) { s.IsRetry = val }
}

func WithFromSummary(val bool) StepOption {
	return func(s *Step) { s.FromSummary = val }
}
