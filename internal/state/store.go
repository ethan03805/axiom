package state

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides durable state management backed by SQLite.
type Store struct {
	db *sql.DB
}

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

// New creates a new Store connected to the given SQLite database path.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Migrate applies the schema migrations to the database.
func (s *Store) Migrate(schema string) error {
	_, err := s.db.Exec(schema)
	return err
}

// CreateTask inserts a new task into the database.
func (s *Store) CreateTask(task *Task) error {
	return nil
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(id string) (*Task, error) {
	return nil, nil
}

// UpdateTaskStatus updates the status of a task.
func (s *Store) UpdateTaskStatus(id, status string) error {
	return nil
}

// ListTasks returns tasks matching the given status filter.
func (s *Store) ListTasks(status string) ([]*Task, error) {
	return nil, nil
}

// CreateAttempt inserts a new task attempt.
func (s *Store) CreateAttempt(attempt *TaskAttempt) error {
	return nil
}

// RecordValidation inserts a validation run result.
func (s *Store) RecordValidation(run *ValidationRun) error {
	return nil
}

// RecordReview inserts a review run result.
func (s *Store) RecordReview(run *ReviewRun) error {
	return nil
}

// RecordArtifact inserts a task artifact record.
func (s *Store) RecordArtifact(artifact *TaskArtifact) error {
	return nil
}

// RecordCost inserts a cost log entry.
func (s *Store) RecordCost(entry *CostEntry) error {
	return nil
}

// AcquireLock attempts to acquire a resource lock for a task.
func (s *Store) AcquireLock(resourceType, resourceKey, taskID string) (bool, error) {
	return false, nil
}

// ReleaseLock releases a resource lock held by a task.
func (s *Store) ReleaseLock(resourceType, resourceKey, taskID string) error {
	return nil
}
