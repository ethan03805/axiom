package state

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertTaskAttempt inserts a new task attempt and sets its ID from LastInsertId.
// Uses the write mutex to prevent SQLITE_BUSY during concurrent task dispatch.
func (db *DB) InsertTaskAttempt(attempt *TaskAttempt) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	result, err := db.conn.Exec(
		`INSERT INTO task_attempts (task_id, attempt_number, model_id, model_family, base_snapshot, status, input_tokens, output_tokens, cost_usd, failure_reason, feedback, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		attempt.TaskID, attempt.AttemptNumber, attempt.ModelID, attempt.ModelFamily,
		attempt.BaseSnapshot, attempt.Status, attempt.InputTokens, attempt.OutputTokens,
		attempt.CostUSD, nullString(attempt.FailureReason), nullString(attempt.Feedback),
		attempt.StartedAt, attempt.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("insert task attempt: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		attempt.ID = id
	}
	return nil
}

// UpdateTaskAttemptStatus updates the status and failure reason of a task attempt.
func (db *DB) UpdateTaskAttemptStatus(id int64, status string, failureReason string) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE task_attempts SET status = ?, failure_reason = ? WHERE id = ?`,
		status, nullString(failureReason), id,
	)
	if err != nil {
		return fmt.Errorf("update task attempt status: %w", err)
	}
	return nil
}

// UpdateTaskAttemptCompleted marks an attempt as completed with final token/cost data.
func (db *DB) UpdateTaskAttemptCompleted(id int64, status string, inputTokens, outputTokens int, costUSD float64) error {
	now := time.Now()
	db.wmu.Lock()
	defer db.wmu.Unlock()
	_, err := db.conn.Exec(
		`UPDATE task_attempts SET status = ?, input_tokens = ?, output_tokens = ?, cost_usd = ?, completed_at = ? WHERE id = ?`,
		status, inputTokens, outputTokens, costUSD, now, id,
	)
	if err != nil {
		return fmt.Errorf("update task attempt completed: %w", err)
	}
	return nil
}

// GetTaskAttempts returns all attempts for a given task, ordered by attempt number.
func (db *DB) GetTaskAttempts(taskID string) ([]*TaskAttempt, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, attempt_number, model_id, model_family, base_snapshot, status, input_tokens, output_tokens, cost_usd, failure_reason, feedback, started_at, completed_at
		 FROM task_attempts WHERE task_id = ? ORDER BY attempt_number`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get task attempts: %w", err)
	}
	defer rows.Close()

	var attempts []*TaskAttempt
	for rows.Next() {
		a := &TaskAttempt{}
		var failureReason, feedback sql.NullString
		err := rows.Scan(
			&a.ID, &a.TaskID, &a.AttemptNumber, &a.ModelID, &a.ModelFamily,
			&a.BaseSnapshot, &a.Status, &a.InputTokens, &a.OutputTokens,
			&a.CostUSD, &failureReason, &feedback, &a.StartedAt, &a.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task attempt: %w", err)
		}
		a.FailureReason = failureReason.String
		a.Feedback = feedback.String
		attempts = append(attempts, a)
	}
	return attempts, rows.Err()
}

// CountTaskAttempts returns the total number of attempts for a given task.
func (db *DB) CountTaskAttempts(taskID string) (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM task_attempts WHERE task_id = ?`, taskID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count task attempts: %w", err)
	}
	return count, nil
}

// CountTaskAttemptsForTier returns the number of attempts for a task at a specific tier.
func (db *DB) CountTaskAttemptsForTier(taskID, tier string) (int, error) {
	// We map tier to model patterns. Attempts record the model_id, not the tier
	// directly. Count all attempts for the task where the model matches the tier's
	// default model, or more reliably, count attempts grouped by attempt_number
	// after a tier change. The simplest approach: count all attempts for a task
	// where the task was at the given tier (based on the model_id used).
	tierModels := map[string][]string{
		"local":    {"anthropic/claude-haiku-4.5"},
		"cheap":    {"anthropic/claude-haiku-4.5"},
		"standard": {"anthropic/claude-sonnet-4"},
		"premium":  {"anthropic/claude-opus-4"},
	}
	models, ok := tierModels[tier]
	if !ok || len(models) == 0 {
		return 0, nil
	}
	var count int
	// Count attempts using any model in this tier's set.
	query := `SELECT COUNT(*) FROM task_attempts WHERE task_id = ? AND model_id IN (?`
	args := []interface{}{taskID, models[0]}
	for _, m := range models[1:] {
		query += ",?"
		args = append(args, m)
	}
	query += ")"
	err := db.conn.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count task attempts for tier: %w", err)
	}
	return count, nil
}

// UpdateTaskTier updates the model tier of a task.
func (db *DB) UpdateTaskTier(taskID, tier string) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	_, err := db.conn.Exec("UPDATE tasks SET tier = ? WHERE id = ?", tier, taskID)
	if err != nil {
		return fmt.Errorf("update task tier: %w", err)
	}
	return nil
}

// InsertValidationRun inserts a validation run and sets its ID from LastInsertId.
func (db *DB) InsertValidationRun(run *ValidationRun) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	result, err := db.conn.Exec(
		`INSERT INTO validation_runs (attempt_id, check_type, status, output, duration_ms, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		run.AttemptID, run.CheckType, run.Status, run.Output, run.DurationMs, run.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert validation run: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		run.ID = id
	}
	return nil
}

// GetValidationRuns returns all validation runs for a given attempt, ordered by timestamp.
func (db *DB) GetValidationRuns(attemptID int64) ([]*ValidationRun, error) {
	rows, err := db.conn.Query(
		`SELECT id, attempt_id, check_type, status, output, duration_ms, timestamp
		 FROM validation_runs WHERE attempt_id = ? ORDER BY timestamp`,
		attemptID,
	)
	if err != nil {
		return nil, fmt.Errorf("get validation runs: %w", err)
	}
	defer rows.Close()

	var runs []*ValidationRun
	for rows.Next() {
		r := &ValidationRun{}
		err := rows.Scan(
			&r.ID, &r.AttemptID, &r.CheckType, &r.Status, &r.Output, &r.DurationMs, &r.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("scan validation run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// InsertReviewRun inserts a review run and sets its ID from LastInsertId.
func (db *DB) InsertReviewRun(run *ReviewRun) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	result, err := db.conn.Exec(
		`INSERT INTO review_runs (attempt_id, reviewer_model, reviewer_family, verdict, feedback, cost_usd, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.AttemptID, run.ReviewerModel, run.ReviewerFamily, run.Verdict,
		run.Feedback, run.CostUSD, run.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert review run: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		run.ID = id
	}
	return nil
}

// GetReviewRuns returns all review runs for a given attempt, ordered by timestamp.
func (db *DB) GetReviewRuns(attemptID int64) ([]*ReviewRun, error) {
	rows, err := db.conn.Query(
		`SELECT id, attempt_id, reviewer_model, reviewer_family, verdict, feedback, cost_usd, timestamp
		 FROM review_runs WHERE attempt_id = ? ORDER BY timestamp`,
		attemptID,
	)
	if err != nil {
		return nil, fmt.Errorf("get review runs: %w", err)
	}
	defer rows.Close()

	var runs []*ReviewRun
	for rows.Next() {
		r := &ReviewRun{}
		err := rows.Scan(
			&r.ID, &r.AttemptID, &r.ReviewerModel, &r.ReviewerFamily,
			&r.Verdict, &r.Feedback, &r.CostUSD, &r.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("scan review run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// InsertTaskArtifact inserts a task artifact and sets its ID from LastInsertId.
func (db *DB) InsertTaskArtifact(artifact *TaskArtifact) error {
	result, err := db.conn.Exec(
		`INSERT INTO task_artifacts (attempt_id, file_path, operation, sha256, size_bytes, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		artifact.AttemptID, artifact.FilePath, artifact.Operation,
		artifact.SHA256, artifact.SizeBytes, artifact.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert task artifact: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		artifact.ID = id
	}
	return nil
}

// GetTaskArtifacts returns all artifacts for a given attempt, ordered by timestamp.
func (db *DB) GetTaskArtifacts(attemptID int64) ([]*TaskArtifact, error) {
	rows, err := db.conn.Query(
		`SELECT id, attempt_id, file_path, operation, sha256, size_bytes, timestamp
		 FROM task_artifacts WHERE attempt_id = ? ORDER BY timestamp`,
		attemptID,
	)
	if err != nil {
		return nil, fmt.Errorf("get task artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []*TaskArtifact
	for rows.Next() {
		a := &TaskArtifact{}
		var sha256 sql.NullString
		var sizeBytes sql.NullInt64
		err := rows.Scan(
			&a.ID, &a.AttemptID, &a.FilePath, &a.Operation, &sha256, &sizeBytes, &a.Timestamp,
		)
		if err != nil {
			return nil, fmt.Errorf("scan task artifact: %w", err)
		}
		a.SHA256 = sha256.String
		a.SizeBytes = sizeBytes.Int64
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}
