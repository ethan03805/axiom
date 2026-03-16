package state

import (
	"database/sql"
	"fmt"
	"time"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusQueued       TaskStatus = "queued"
	TaskStatusInProgress   TaskStatus = "in_progress"
	TaskStatusInReview     TaskStatus = "in_review"
	TaskStatusDone         TaskStatus = "done"
	TaskStatusFailed       TaskStatus = "failed"
	TaskStatusBlocked      TaskStatus = "blocked"
	TaskStatusWaitingOnLock TaskStatus = "waiting_on_lock"
	TaskStatusCancelledECO TaskStatus = "cancelled_eco"
)

// validTransitions defines the allowed state transitions for tasks.
var validTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusQueued:       {TaskStatusInProgress},
	TaskStatusInProgress:   {TaskStatusInReview, TaskStatusFailed, TaskStatusBlocked, TaskStatusWaitingOnLock},
	TaskStatusInReview:     {TaskStatusDone, TaskStatusFailed, TaskStatusBlocked},
	TaskStatusFailed:       {TaskStatusQueued},
	TaskStatusWaitingOnLock: {TaskStatusQueued},
}

// activeStates are states that can transition to cancelled_eco.
var activeStates = map[TaskStatus]bool{
	TaskStatusQueued:       true,
	TaskStatusInProgress:   true,
	TaskStatusInReview:     true,
	TaskStatusBlocked:      true,
	TaskStatusWaitingOnLock: true,
	TaskStatusFailed:       true,
}

// TaskFilter defines filtering criteria for listing tasks.
type TaskFilter struct {
	Status   TaskStatus
	ParentID string
	TaskType string
}

// CreateTask inserts a new task into the database.
func (db *DB) CreateTask(task *Task) error {
	_, err := db.conn.Exec(
		`INSERT INTO tasks (id, parent_id, title, description, status, tier, task_type, base_snapshot, eco_ref, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, nullString(task.ParentID), task.Title, task.Description,
		task.Status, task.Tier, task.TaskType, task.BaseSnapshot,
		nullString(task.EcoRef), task.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}

// CreateTaskBatch inserts multiple tasks atomically in a single transaction.
func (db *DB) CreateTaskBatch(tasks []*Task) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO tasks (id, parent_id, title, description, status, tier, task_type, base_snapshot, eco_ref, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, task := range tasks {
		_, err := stmt.Exec(
			task.ID, nullString(task.ParentID), task.Title, task.Description,
			task.Status, task.Tier, task.TaskType, task.BaseSnapshot,
			nullString(task.EcoRef), task.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert task %s: %w", task.ID, err)
		}
	}

	return tx.Commit()
}

// GetTask retrieves a task by ID.
func (db *DB) GetTask(id string) (*Task, error) {
	row := db.conn.QueryRow(
		`SELECT id, parent_id, title, description, status, tier, task_type, base_snapshot, eco_ref, created_at, completed_at
		 FROM tasks WHERE id = ?`, id,
	)

	task := &Task{}
	var parentID, ecoRef sql.NullString
	err := row.Scan(
		&task.ID, &parentID, &task.Title, &task.Description,
		&task.Status, &task.Tier, &task.TaskType, &task.BaseSnapshot,
		&ecoRef, &task.CreatedAt, &task.CompletedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	task.ParentID = parentID.String
	task.EcoRef = ecoRef.String
	return task, nil
}

// ListTasks returns tasks matching the given filter criteria.
func (db *DB) ListTasks(filter TaskFilter) ([]*Task, error) {
	query := `SELECT id, parent_id, title, description, status, tier, task_type, base_snapshot, eco_ref, created_at, completed_at FROM tasks WHERE 1=1`
	var args []interface{}

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, string(filter.Status))
	}
	if filter.ParentID != "" {
		query += " AND parent_id = ?"
		args = append(args, filter.ParentID)
	}
	if filter.TaskType != "" {
		query += " AND task_type = ?"
		args = append(args, filter.TaskType)
	}

	query += " ORDER BY created_at"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task := &Task{}
		var parentID, ecoRef sql.NullString
		err := rows.Scan(
			&task.ID, &parentID, &task.Title, &task.Description,
			&task.Status, &task.Tier, &task.TaskType, &task.BaseSnapshot,
			&ecoRef, &task.CreatedAt, &task.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		task.ParentID = parentID.String
		task.EcoRef = ecoRef.String
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus updates a task's status with state machine validation.
func (db *DB) UpdateTaskStatus(id string, status TaskStatus) error {
	// Get current status
	var currentStatus string
	err := db.conn.QueryRow("SELECT status FROM tasks WHERE id = ?", id).Scan(&currentStatus)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found: %s", id)
	}
	if err != nil {
		return fmt.Errorf("get task status: %w", err)
	}

	current := TaskStatus(currentStatus)

	// Check for cancelled_eco (any active state can transition to it)
	if status == TaskStatusCancelledECO {
		if !activeStates[current] {
			return fmt.Errorf("invalid transition: %s -> %s", current, status)
		}
	} else {
		// Check valid transitions
		allowed, ok := validTransitions[current]
		if !ok {
			return fmt.Errorf("invalid transition: %s -> %s (no transitions from %s)", current, status, current)
		}
		valid := false
		for _, s := range allowed {
			if s == status {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid transition: %s -> %s", current, status)
		}
	}

	// Update status (and completed_at if done)
	if status == TaskStatusDone {
		now := time.Now()
		_, err = db.conn.Exec("UPDATE tasks SET status = ?, completed_at = ? WHERE id = ?", string(status), now, id)
	} else {
		_, err = db.conn.Exec("UPDATE tasks SET status = ? WHERE id = ?", string(status), id)
	}
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return nil
}

// GetReadyTasks returns tasks in 'queued' status whose dependencies are all 'done'.
func (db *DB) GetReadyTasks() ([]*Task, error) {
	rows, err := db.conn.Query(`
		SELECT t.id, t.parent_id, t.title, t.description, t.status, t.tier,
		       t.task_type, t.base_snapshot, t.eco_ref, t.created_at, t.completed_at
		FROM tasks t
		WHERE t.status = 'queued'
		  AND NOT EXISTS (
		    SELECT 1 FROM task_dependencies td
		    JOIN tasks dep ON dep.id = td.depends_on
		    WHERE td.task_id = t.id AND dep.status != 'done'
		  )
		ORDER BY t.created_at
	`)
	if err != nil {
		return nil, fmt.Errorf("get ready tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		task := &Task{}
		var parentID, ecoRef sql.NullString
		err := rows.Scan(
			&task.ID, &parentID, &task.Title, &task.Description,
			&task.Status, &task.Tier, &task.TaskType, &task.BaseSnapshot,
			&ecoRef, &task.CreatedAt, &task.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		task.ParentID = parentID.String
		task.EcoRef = ecoRef.String
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// AddTaskDependency adds a dependency between tasks with circular dependency detection.
func (db *DB) AddTaskDependency(taskID, dependsOn string) error {
	// Check that both tasks exist
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM tasks WHERE id IN (?, ?)", taskID, dependsOn).Scan(&count)
	if err != nil {
		return fmt.Errorf("check tasks: %w", err)
	}
	if count < 2 && taskID != dependsOn {
		return fmt.Errorf("one or both tasks do not exist: %s, %s", taskID, dependsOn)
	}

	// Self-dependency check
	if taskID == dependsOn {
		return fmt.Errorf("circular dependency detected: task %s cannot depend on itself", taskID)
	}

	// DFS from dependsOn to detect if we can reach taskID (circular dep)
	if err := db.detectCircularDep(taskID, dependsOn); err != nil {
		return err
	}

	_, err = db.conn.Exec(
		"INSERT OR IGNORE INTO task_dependencies (task_id, depends_on) VALUES (?, ?)",
		taskID, dependsOn,
	)
	if err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return nil
}

// detectCircularDep does a DFS from dependsOn following its own dependencies.
// If we reach taskID, there's a circular dependency.
func (db *DB) detectCircularDep(taskID, dependsOn string) error {
	visited := make(map[string]bool)
	stack := []string{dependsOn}

	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if current == taskID {
			return fmt.Errorf("circular dependency detected: adding %s -> %s would create a cycle", taskID, dependsOn)
		}

		if visited[current] {
			continue
		}
		visited[current] = true

		rows, err := db.conn.Query("SELECT depends_on FROM task_dependencies WHERE task_id = ?", current)
		if err != nil {
			return fmt.Errorf("query deps: %w", err)
		}

		var deps []string
		for rows.Next() {
			var dep string
			if err := rows.Scan(&dep); err != nil {
				rows.Close()
				return fmt.Errorf("scan dep: %w", err)
			}
			deps = append(deps, dep)
		}
		rows.Close()

		stack = append(stack, deps...)
	}
	return nil
}

// AddTaskSRSRef adds an SRS reference to a task.
func (db *DB) AddTaskSRSRef(taskID, srsRef string) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO task_srs_refs (task_id, srs_ref) VALUES (?, ?)",
		taskID, srsRef,
	)
	if err != nil {
		return fmt.Errorf("add srs ref: %w", err)
	}
	return nil
}

// AddTaskTargetFile adds a target file to a task.
func (db *DB) AddTaskTargetFile(taskID, filePath, lockScope string) error {
	if lockScope == "" {
		lockScope = "file"
	}
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO task_target_files (task_id, file_path, lock_scope) VALUES (?, ?, ?)",
		taskID, filePath, lockScope,
	)
	if err != nil {
		return fmt.Errorf("add target file: %w", err)
	}
	return nil
}

// nullString returns a sql.NullString for optional string fields.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

