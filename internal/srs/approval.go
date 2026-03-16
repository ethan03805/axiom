package srs

import (
	"fmt"

	"github.com/ethan03805/axiom/internal/events"
)

// ApprovalDelegate identifies who approves the SRS.
// See Architecture Section 8.5.
type ApprovalDelegate string

const (
	DelegateUser ApprovalDelegate = "user"
	DelegateClaw ApprovalDelegate = "claw"
)

// ApprovalManager handles the SRS approval flow.
// See Architecture Section 5.1 steps 3-5 and Section 8.5.
type ApprovalManager struct {
	lock     *LockManager
	emitter  *events.Emitter
	delegate ApprovalDelegate
}

// NewApprovalManager creates an ApprovalManager.
func NewApprovalManager(axiomDir string, emitter *events.Emitter, delegate ApprovalDelegate) *ApprovalManager {
	if delegate == "" {
		delegate = DelegateUser
	}
	return &ApprovalManager{
		lock:     NewLockManager(axiomDir),
		emitter:  emitter,
		delegate: delegate,
	}
}

// SubmitDraft writes an SRS draft for review. Validates format first.
// Returns validation errors if the SRS is malformed.
// See Architecture Section 5.1 step 3.
func (am *ApprovalManager) SubmitDraft(content string) ([]string, error) {
	// Validate format.
	formatErrors := ValidateFormat(content)
	if len(formatErrors) > 0 {
		return formatErrors, nil
	}

	// Write as draft (writable).
	if err := am.lock.WriteDraft(content); err != nil {
		return nil, fmt.Errorf("write draft: %w", err)
	}

	am.emitter.Emit(events.Event{
		Type: events.EventSRSSubmitted,
		Details: map[string]interface{}{
			"status": "draft",
		},
	})

	return nil, nil
}

// Approve locks the SRS and makes it immutable.
// Returns the SHA-256 hash for storage in SQLite.
// See Architecture Section 5.1 step 5 and Section 6.2.
func (am *ApprovalManager) Approve(approvedBy string) (string, error) {
	if am.lock.IsLocked() {
		return "", fmt.Errorf("SRS is already approved and locked")
	}

	hash, err := am.lock.Lock()
	if err != nil {
		return "", fmt.Errorf("lock SRS: %w", err)
	}

	am.emitter.Emit(events.Event{
		Type: events.EventSRSApproved,
		Details: map[string]interface{}{
			"approved_by": approvedBy,
			"delegate":    string(am.delegate),
			"sha256":      hash,
		},
	})

	return hash, nil
}

// Reject sends feedback to the orchestrator for revision.
// See Architecture Section 5.1 step 4.
func (am *ApprovalManager) Reject(feedback string) error {
	am.emitter.Emit(events.Event{
		Type: events.EventSRSSubmitted,
		Details: map[string]interface{}{
			"status":   "rejected",
			"feedback": feedback,
		},
	})
	return nil
}

// IsApproved returns true if the SRS has been approved and locked.
func (am *ApprovalManager) IsApproved() bool {
	return am.lock.IsLocked()
}

// IsDelegatedToClaw returns true if SRS approval is delegated to a Claw.
func (am *ApprovalManager) IsDelegatedToClaw() bool {
	return am.delegate == DelegateClaw
}

// VerifyIntegrity checks the SRS hash on engine startup.
func (am *ApprovalManager) VerifyIntegrity() error {
	return am.lock.VerifyIntegrity()
}
