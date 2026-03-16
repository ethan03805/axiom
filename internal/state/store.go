package state

import (
	"time"
)

// Task represents a unit of work in the system.
type Task struct {
	ID           string
	ParentID     string
	Title        string
	Description  string
	Status       string
	Tier         string
	TaskType     string
	BaseSnapshot string
	EcoRef       string
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

// TaskAttempt represents a single execution attempt of a task.
type TaskAttempt struct {
	ID            int64
	TaskID        string
	AttemptNumber int
	ModelID       string
	ModelFamily   string
	BaseSnapshot  string
	Status        string
	InputTokens   int
	OutputTokens  int
	CostUSD       float64
	FailureReason string
	Feedback      string
	StartedAt     time.Time
	CompletedAt   *time.Time
}

// ValidationRun records the result of a validation check.
type ValidationRun struct {
	ID         int64
	AttemptID  int64
	CheckType  string
	Status     string
	Output     string
	DurationMs int
	Timestamp  time.Time
}

// ReviewRun records the result of a code review.
type ReviewRun struct {
	ID             int64
	AttemptID      int64
	ReviewerModel  string
	ReviewerFamily string
	Verdict        string
	Feedback       string
	CostUSD        float64
	Timestamp      time.Time
}

// TaskArtifact records a file produced or modified by a task attempt.
type TaskArtifact struct {
	ID        int64
	AttemptID int64
	FilePath  string
	Operation string
	SHA256    string
	SizeBytes int64
	Timestamp time.Time
}

// ContainerSession records container lifecycle information.
type ContainerSession struct {
	ID            string
	TaskID        string
	ContainerType string
	Image         string
	ModelID       string
	CPULimit      float64
	MemLimit      string
	StartedAt     time.Time
	StoppedAt     *time.Time
	ExitReason    string
}

// CostEntry records token usage and cost for billing.
type CostEntry struct {
	ID           int64
	TaskID       string
	AttemptID    int64
	AgentType    string
	ModelID      string
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Timestamp    time.Time
}

// EcoEntry records an engineering change order.
type EcoEntry struct {
	ID             int64
	EcoCode        string
	Category       string
	Description    string
	AffectedRefs   string
	ProposedChange string
	Status         string
	ApprovedBy     string
	CreatedAt      time.Time
	ResolvedAt     *time.Time
}
