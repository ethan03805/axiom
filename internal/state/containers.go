package state

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertContainerSession records a new container lifecycle entry.
func (db *DB) InsertContainerSession(s *ContainerSession) error {
	_, err := db.conn.Exec(
		`INSERT INTO container_sessions
		 (id, task_id, container_type, image, model_id, cpu_limit, mem_limit, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.TaskID, s.ContainerType, s.Image,
		nullString(s.ModelID), s.CPULimit, s.MemLimit, s.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("insert container session: %w", err)
	}
	return nil
}

// UpdateContainerSessionStopped marks a container session as stopped with an exit reason.
func (db *DB) UpdateContainerSessionStopped(id string, stoppedAt time.Time, exitReason string) error {
	result, err := db.conn.Exec(
		`UPDATE container_sessions SET stopped_at = ?, exit_reason = ? WHERE id = ?`,
		stoppedAt, exitReason, id,
	)
	if err != nil {
		return fmt.Errorf("update container session: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("container session not found: %s", id)
	}
	return nil
}

// GetContainerSession retrieves a container session by ID.
func (db *DB) GetContainerSession(id string) (*ContainerSession, error) {
	row := db.conn.QueryRow(
		`SELECT id, task_id, container_type, image, model_id, cpu_limit, mem_limit,
		        started_at, stopped_at, exit_reason
		 FROM container_sessions WHERE id = ?`, id,
	)

	s := &ContainerSession{}
	var modelID sql.NullString
	var exitReason sql.NullString
	err := row.Scan(
		&s.ID, &s.TaskID, &s.ContainerType, &s.Image,
		&modelID, &s.CPULimit, &s.MemLimit,
		&s.StartedAt, &s.StoppedAt, &exitReason,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("container session not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("get container session: %w", err)
	}
	s.ModelID = modelID.String
	s.ExitReason = exitReason.String
	return s, nil
}

// ListContainerSessionsByTask returns all container sessions for a given task.
func (db *DB) ListContainerSessionsByTask(taskID string) ([]*ContainerSession, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, container_type, image, model_id, cpu_limit, mem_limit,
		        started_at, stopped_at, exit_reason
		 FROM container_sessions WHERE task_id = ? ORDER BY started_at`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list container sessions: %w", err)
	}
	defer rows.Close()

	return scanContainerSessions(rows)
}

// ListActiveContainerSessions returns container sessions that have not been stopped.
func (db *DB) ListActiveContainerSessions() ([]*ContainerSession, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, container_type, image, model_id, cpu_limit, mem_limit,
		        started_at, stopped_at, exit_reason
		 FROM container_sessions WHERE stopped_at IS NULL ORDER BY started_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active container sessions: %w", err)
	}
	defer rows.Close()

	return scanContainerSessions(rows)
}

// MarkOrphanedContainerSessions marks all active sessions as orphaned.
// Called during crash recovery when no containers survive a restart.
func (db *DB) MarkOrphanedContainerSessions() (int64, error) {
	now := time.Now()
	result, err := db.conn.Exec(
		`UPDATE container_sessions SET stopped_at = ?, exit_reason = 'orphaned'
		 WHERE stopped_at IS NULL`, now,
	)
	if err != nil {
		return 0, fmt.Errorf("mark orphaned sessions: %w", err)
	}
	count, _ := result.RowsAffected()
	return count, nil
}

func scanContainerSessions(rows *sql.Rows) ([]*ContainerSession, error) {
	var sessions []*ContainerSession
	for rows.Next() {
		s := &ContainerSession{}
		var modelID sql.NullString
		var exitReason sql.NullString
		err := rows.Scan(
			&s.ID, &s.TaskID, &s.ContainerType, &s.Image,
			&modelID, &s.CPULimit, &s.MemLimit,
			&s.StartedAt, &s.StoppedAt, &exitReason,
		)
		if err != nil {
			return nil, fmt.Errorf("scan container session: %w", err)
		}
		s.ModelID = modelID.String
		s.ExitReason = exitReason.String
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
