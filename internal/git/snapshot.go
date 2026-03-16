package git

import (
	"fmt"
)

// SnapshotManager tracks base snapshots for TaskSpecs and validates
// them against the current HEAD during merge queue processing.
// See Architecture Section 16.2 (Base Snapshot Pinning).
type SnapshotManager struct {
	gitMgr *Manager
}

// NewSnapshotManager creates a SnapshotManager.
func NewSnapshotManager(gitMgr *Manager) *SnapshotManager {
	return &SnapshotManager{gitMgr: gitMgr}
}

// CurrentSnapshot returns the current HEAD SHA for use as a base snapshot
// in a new TaskSpec.
func (sm *SnapshotManager) CurrentSnapshot() (string, error) {
	return sm.gitMgr.HeadSHA()
}

// SnapshotStatus represents the result of comparing a base snapshot to HEAD.
type SnapshotStatus int

const (
	// SnapshotCurrent means the base snapshot matches HEAD (no changes since).
	SnapshotCurrent SnapshotStatus = iota
	// SnapshotStale means HEAD has advanced but the snapshot is an ancestor
	// (clean merge may be possible).
	SnapshotStale
	// SnapshotDiverged means the snapshot is not an ancestor of HEAD
	// (should not happen in normal operation; indicates corruption or
	// force-push).
	SnapshotDiverged
)

// ValidateSnapshot checks whether a base snapshot is current, stale, or diverged
// relative to the current HEAD.
// See Architecture Section 16.2.
func (sm *SnapshotManager) ValidateSnapshot(baseSnapshot string) (SnapshotStatus, error) {
	head, err := sm.gitMgr.HeadSHA()
	if err != nil {
		return SnapshotDiverged, fmt.Errorf("get HEAD: %w", err)
	}

	// If base snapshot matches HEAD, it's current.
	if baseSnapshot == head {
		return SnapshotCurrent, nil
	}

	// Check if base snapshot is an ancestor of HEAD (stale but mergeable).
	isAnc, err := sm.gitMgr.IsAncestor(baseSnapshot, head)
	if err != nil {
		return SnapshotDiverged, fmt.Errorf("ancestry check: %w", err)
	}
	if isAnc {
		return SnapshotStale, nil
	}

	return SnapshotDiverged, nil
}

// ChangedFilesSince returns the list of files that changed between the
// base snapshot and the current HEAD. Used to determine if a stale
// TaskSpec's context needs updating.
func (sm *SnapshotManager) ChangedFilesSince(baseSnapshot string) ([]string, error) {
	head, err := sm.gitMgr.HeadSHA()
	if err != nil {
		return nil, err
	}
	if baseSnapshot == head {
		return nil, nil
	}
	return sm.gitMgr.DiffFiles(baseSnapshot, head)
}
