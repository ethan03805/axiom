package srs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// Valid ECO category codes per Architecture Section 7.2.
// The engine SHALL reject ECOs that do not match a defined category.
var ValidECOCategories = map[string]string{
	"ECO-DEP": "Dependency Unavailable",
	"ECO-API": "API Breaking Change",
	"ECO-SEC": "Security Vulnerability",
	"ECO-PLT": "Platform Incompatibility",
	"ECO-LIC": "License Conflict",
	"ECO-PRV": "Provider Limitation",
}

// ECOManager handles the Engineering Change Order process.
// ECOs provide a controlled mechanism for adapting to external environmental
// realities without violating the immutable-scope principle.
//
// See Architecture Section 7.
type ECOManager struct {
	db      *state.DB
	emitter *events.Emitter
	lock    *LockManager
	ecoDir  string // .axiom/eco/
}

// NewECOManager creates an ECO manager.
func NewECOManager(db *state.DB, emitter *events.Emitter, axiomDir string) *ECOManager {
	return &ECOManager{
		db:      db,
		emitter: emitter,
		lock:    NewLockManager(axiomDir),
		ecoDir:  filepath.Join(axiomDir, "eco"),
	}
}

// ProposeECO validates and records a new ECO proposal.
// Returns an error if the category is invalid.
// See Architecture Section 7.3 steps 1-3.
func (m *ECOManager) ProposeECO(ecoCode, description, affectedRefs, proposedChange string) (*state.EcoEntry, error) {
	// Validate category.
	category, valid := ValidECOCategories[ecoCode]
	if !valid {
		return nil, fmt.Errorf("invalid ECO category: %s (valid: %s)",
			ecoCode, strings.Join(validCategoryCodes(), ", "))
	}

	eco := &state.EcoEntry{
		EcoCode:        ecoCode,
		Category:       category,
		Description:    description,
		AffectedRefs:   affectedRefs,
		ProposedChange: proposedChange,
		Status:         "proposed",
		CreatedAt:      time.Now(),
	}

	if err := m.db.InsertECO(eco); err != nil {
		return nil, fmt.Errorf("insert ECO: %w", err)
	}

	m.emitter.Emit(events.Event{
		Type: events.EventECOProposed,
		Details: map[string]interface{}{
			"eco_id":      eco.ID,
			"eco_code":    ecoCode,
			"category":    category,
			"description": description,
		},
	})

	return eco, nil
}

// ApproveECO approves an ECO, records the addendum, and marks affected tasks.
// See Architecture Section 7.3 steps 4-5.
func (m *ECOManager) ApproveECO(ecoID int64, approvedBy string) error {
	// Update ECO status.
	if err := m.db.UpdateECOStatus(ecoID, "approved", approvedBy); err != nil {
		return fmt.Errorf("approve ECO: %w", err)
	}

	// Get the ECO details for the addendum.
	ecos, err := m.db.ListECOs("")
	if err != nil {
		return fmt.Errorf("list ECOs: %w", err)
	}
	var eco *state.EcoEntry
	for _, e := range ecos {
		if e.ID == ecoID {
			eco = e
			break
		}
	}
	if eco == nil {
		return fmt.Errorf("ECO %d not found", ecoID)
	}

	// Write the ECO record as a versioned addendum file.
	// Original SRS text is preserved (never overwritten).
	// See Architecture Section 7.3 step 5.
	if err := m.writeECOAddendum(eco); err != nil {
		return fmt.Errorf("write ECO addendum: %w", err)
	}

	m.emitter.Emit(events.Event{
		Type: events.EventECOApproved,
		Details: map[string]interface{}{
			"eco_id":      ecoID,
			"approved_by": approvedBy,
		},
	})

	return nil
}

// RejectECO rejects an ECO proposal.
// See Architecture Section 7.3 step 6.
func (m *ECOManager) RejectECO(ecoID int64, rejectedBy string) error {
	if err := m.db.UpdateECOStatus(ecoID, "rejected", rejectedBy); err != nil {
		return fmt.Errorf("reject ECO: %w", err)
	}

	m.emitter.Emit(events.Event{
		Type: events.EventECORejected,
		Details: map[string]interface{}{
			"eco_id":      ecoID,
			"rejected_by": rejectedBy,
		},
	})

	return nil
}

// CancelAffectedTasks marks tasks referenced by an approved ECO as cancelled_eco.
// The orchestrator will create replacement tasks.
// See Architecture Section 15.4 (cancelled_eco state).
func (m *ECOManager) CancelAffectedTasks(ecoID int64, taskIDs []string) error {
	for _, taskID := range taskIDs {
		// Set the task's eco_ref to this ECO.
		_, err := m.db.Conn().Exec(
			"UPDATE tasks SET eco_ref = ? WHERE id = ?", ecoID, taskID,
		)
		if err != nil {
			return fmt.Errorf("set eco_ref for task %s: %w", taskID, err)
		}

		// Transition to cancelled_eco.
		if err := m.db.UpdateTaskStatus(taskID, state.TaskStatusCancelledECO); err != nil {
			return fmt.Errorf("cancel task %s: %w", taskID, err)
		}
	}
	return nil
}

// writeECOAddendum writes an ECO record to .axiom/eco/ECO-NNN.md
// per Architecture Section 7.4.
func (m *ECOManager) writeECOAddendum(eco *state.EcoEntry) error {
	if err := os.MkdirAll(m.ecoDir, 0755); err != nil {
		return fmt.Errorf("create eco dir: %w", err)
	}

	filename := fmt.Sprintf("ECO-%03d.md", eco.ID)
	content := fmt.Sprintf(`## ECO-%03d: [%s] %s

**Filed:** %s
**Status:** Approved
**Affected SRS Sections:** %s

### Environmental Issue
%s

### Proposed Substitute
%s
`, eco.ID, eco.EcoCode, eco.Category,
		eco.CreatedAt.Format(time.RFC3339),
		eco.AffectedRefs,
		eco.Description,
		eco.ProposedChange,
	)

	path := filepath.Join(m.ecoDir, filename)
	return os.WriteFile(path, []byte(content), 0644)
}

// ValidateECOCategory checks if an ECO code is in the allowed list.
func ValidateECOCategory(code string) bool {
	_, valid := ValidECOCategories[code]
	return valid
}

func validCategoryCodes() []string {
	codes := make([]string, 0, len(ValidECOCategories))
	for code := range ValidECOCategories {
		codes = append(codes, code)
	}
	return codes
}
