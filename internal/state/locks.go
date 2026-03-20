package state

import (
	"fmt"
	"sort"
	"time"
)

// LockRequest describes a resource to be locked.
type LockRequest struct {
	ResourceType string
	ResourceKey  string
}

// Lock represents an active resource lock.
type Lock struct {
	ResourceType string
	ResourceKey  string
	TaskID       string
	LockedAt     time.Time
}

// AcquireLocks acquires all requested locks atomically (all-or-nothing).
// Locks are acquired in sorted order by (resource_type, resource_key)
// to prevent deadlocks.
func (db *DB) AcquireLocks(taskID string, locks []LockRequest) error {
	if len(locks) == 0 {
		return nil
	}

	// Sort locks deterministically to prevent deadlocks
	sorted := make([]LockRequest, len(locks))
	copy(sorted, locks)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ResourceType == sorted[j].ResourceType {
			return sorted[i].ResourceKey < sorted[j].ResourceKey
		}
		return sorted[i].ResourceType < sorted[j].ResourceType
	})

	db.wmu.Lock()
	defer db.wmu.Unlock()

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check that none of the resources are already locked
	for _, lock := range sorted {
		var count int
		err := tx.QueryRow(
			"SELECT COUNT(*) FROM task_locks WHERE resource_type = ? AND resource_key = ?",
			lock.ResourceType, lock.ResourceKey,
		).Scan(&count)
		if err != nil {
			return fmt.Errorf("check lock: %w", err)
		}
		if count > 0 {
			return fmt.Errorf("resource already locked: %s/%s", lock.ResourceType, lock.ResourceKey)
		}
	}

	// All clear, acquire all locks
	for _, lock := range sorted {
		_, err := tx.Exec(
			"INSERT INTO task_locks (resource_type, resource_key, task_id) VALUES (?, ?, ?)",
			lock.ResourceType, lock.ResourceKey, taskID,
		)
		if err != nil {
			return fmt.Errorf("insert lock: %w", err)
		}
	}

	return tx.Commit()
}

// ReleaseLocks releases all locks held by the given task.
func (db *DB) ReleaseLocks(taskID string) error {
	db.wmu.Lock()
	defer db.wmu.Unlock()
	_, err := db.conn.Exec("DELETE FROM task_locks WHERE task_id = ?", taskID)
	if err != nil {
		return fmt.Errorf("release locks: %w", err)
	}
	return nil
}

// GetLockedResources returns all active locks.
func (db *DB) GetLockedResources() ([]Lock, error) {
	rows, err := db.conn.Query(
		"SELECT resource_type, resource_key, task_id, locked_at FROM task_locks ORDER BY resource_type, resource_key",
	)
	if err != nil {
		return nil, fmt.Errorf("get locks: %w", err)
	}
	defer rows.Close()

	var locks []Lock
	for rows.Next() {
		var l Lock
		if err := rows.Scan(&l.ResourceType, &l.ResourceKey, &l.TaskID, &l.LockedAt); err != nil {
			return nil, fmt.Errorf("scan lock: %w", err)
		}
		locks = append(locks, l)
	}
	return locks, rows.Err()
}

// CheckAllLocksAvailable checks if ALL the requested locks can be acquired
// by the given task. Locks already held by requestingTaskID are treated as
// available (the task can re-acquire its own locks). This prevents stale
// self-held locks from permanently blocking a re-queued task.
// Returns true if all are available, false if any are held by another task.
// If unavailable, returns the task ID holding the first conflicting lock.
func (db *DB) CheckAllLocksAvailable(locks []LockRequest, requestingTaskID string) (bool, string, error) {
	for _, lock := range locks {
		locked, holder, err := db.IsLocked(lock.ResourceType, lock.ResourceKey)
		if err != nil {
			return false, "", err
		}
		if locked && holder != requestingTaskID {
			return false, holder, nil
		}
	}
	return true, "", nil
}

// BuildLocksFromTargetFiles converts a task's target files into lock requests
// using each file's declared lock scope.
func (db *DB) BuildLocksFromTargetFiles(taskID string) ([]LockRequest, error) {
	files, err := db.GetTaskTargetFiles(taskID)
	if err != nil {
		return nil, err
	}
	locks := make([]LockRequest, len(files))
	for i, f := range files {
		locks[i] = LockRequest{
			ResourceType: f.LockScope,
			ResourceKey:  f.FilePath,
		}
	}
	return locks, nil
}

// GetLocksByTask returns all locks currently held by a specific task.
func (db *DB) GetLocksByTask(taskID string) ([]Lock, error) {
	rows, err := db.conn.Query(
		"SELECT resource_type, resource_key, task_id, locked_at FROM task_locks WHERE task_id = ? ORDER BY resource_type, resource_key",
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get locks by task: %w", err)
	}
	defer rows.Close()

	var locks []Lock
	for rows.Next() {
		var l Lock
		if err := rows.Scan(&l.ResourceType, &l.ResourceKey, &l.TaskID, &l.LockedAt); err != nil {
			return nil, fmt.Errorf("scan lock: %w", err)
		}
		locks = append(locks, l)
	}
	return locks, rows.Err()
}

// IsLocked checks if a resource is currently locked.
// Returns whether it's locked and the holding task ID.
func (db *DB) IsLocked(resourceType, resourceKey string) (bool, string, error) {
	var taskID string
	err := db.conn.QueryRow(
		"SELECT task_id FROM task_locks WHERE resource_type = ? AND resource_key = ?",
		resourceType, resourceKey,
	).Scan(&taskID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, "", nil
		}
		return false, "", fmt.Errorf("check lock: %w", err)
	}
	return true, taskID, nil
}
