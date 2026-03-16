package srs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// validSRS returns a minimal valid SRS document matching Architecture Section 6.1.
func validSRS() string {
	return `# SRS: Test Project

## 1. Architecture

### 1.1 System Overview
A test system.

### 1.2 Component Breakdown
- Component A: does things

### 1.3 Technology Decisions
Go for backend.

### 1.4 Data Model
SQLite database.

### 1.5 Directory Structure
Standard Go layout.

## 2. Requirements & Constraints

### 2.1 Functional Requirements
- FR-001: The system SHALL handle requests.
- FR-002: The system SHALL store data.

### 2.2 Non-Functional Requirements
- NFR-001: The system SHALL respond within 100ms.

### 2.3 Constraints
No external dependencies.

### 2.4 Assumptions
Network available.

## 3. Test Strategy

### 3.1 Unit Testing
Go testing package.

### 3.2 Integration Testing
Docker-based.

## 4. Acceptance Criteria

### 4.1 Per-Component Criteria
- AC-001: Component A handles 100 requests.
- AC-002: Data persists across restarts.

### 4.2 Integration Criteria
- IC-001: End-to-end request flow works.

### 4.3 Completion Definition
All tests pass.
`
}

func TestValidateFormatValid(t *testing.T) {
	errors := ValidateFormat(validSRS())
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid SRS, got: %v", errors)
	}
}

func TestValidateFormatEmpty(t *testing.T) {
	errors := ValidateFormat("")
	if len(errors) == 0 {
		t.Error("expected error for empty SRS")
	}
}

func TestValidateFormatMissingTitle(t *testing.T) {
	content := strings.Replace(validSRS(), "# SRS: Test Project", "# My Document", 1)
	errors := ValidateFormat(content)
	found := false
	for _, e := range errors {
		if strings.Contains(e, "SRS must start with") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected title error, got: %v", errors)
	}
}

func TestValidateFormatMissingSections(t *testing.T) {
	// Remove the test strategy section.
	content := strings.Replace(validSRS(), "## 3. Test Strategy", "## 3. Something Else", 1)
	errors := ValidateFormat(content)
	found := false
	for _, e := range errors {
		if strings.Contains(e, "## 3. Test Strategy") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing section error, got: %v", errors)
	}
}

func TestValidateFormatMissingSubsections(t *testing.T) {
	content := strings.Replace(validSRS(), "### 1.4 Data Model", "### 1.4 Something", 1)
	errors := ValidateFormat(content)
	found := false
	for _, e := range errors {
		if strings.Contains(e, "### 1.4 Data Model") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing subsection error, got: %v", errors)
	}
}

func TestValidateFormatNoRequirements(t *testing.T) {
	// Remove all FR-xxx patterns.
	content := strings.ReplaceAll(validSRS(), "FR-001", "Req-001")
	content = strings.ReplaceAll(content, "FR-002", "Req-002")
	errors := ValidateFormat(content)
	found := false
	for _, e := range errors {
		if strings.Contains(e, "no functional requirements") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected no FR error, got: %v", errors)
	}
}

func TestExtractRequirementIDs(t *testing.T) {
	frs, nfrs, acs, ics := ExtractRequirementIDs(validSRS())
	if len(frs) != 2 {
		t.Errorf("expected 2 FRs, got %d: %v", len(frs), frs)
	}
	if len(nfrs) != 1 {
		t.Errorf("expected 1 NFR, got %d", len(nfrs))
	}
	if len(acs) != 2 {
		t.Errorf("expected 2 ACs, got %d", len(acs))
	}
	if len(ics) != 1 {
		t.Errorf("expected 1 IC, got %d", len(ics))
	}
}

func TestValidateRequirementRef(t *testing.T) {
	tests := []struct {
		ref   string
		valid bool
	}{
		{"FR-001", true},
		{"NFR-042", true},
		{"AC-003", true},
		{"IC-001", true},
		{"INVALID", false},
		{"FR001", false},
		{"", false},
	}
	for _, tt := range tests {
		if ValidateRequirementRef(tt.ref) != tt.valid {
			t.Errorf("ValidateRequirementRef(%s) = %v, want %v", tt.ref, !tt.valid, tt.valid)
		}
	}
}

// --- Lock Manager tests ---

func TestLockManagerWriteAndLock(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-srs-lock-*")
	defer os.RemoveAll(tmpDir)

	lm := NewLockManager(tmpDir)

	// Write draft.
	if err := lm.WriteDraft("# SRS: Test"); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	// Should not be locked yet.
	if lm.IsLocked() {
		t.Error("SRS should not be locked before approval")
	}

	// Lock.
	hash, err := lm.Lock()
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	// Should now be locked.
	if !lm.IsLocked() {
		t.Error("SRS should be locked after approval")
	}

	// Hash file should exist.
	if _, err := os.Stat(lm.HashPath()); os.IsNotExist(err) {
		t.Error("hash file should exist")
	}
}

func TestLockManagerVerifyIntegrity(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-srs-integrity-*")
	defer os.RemoveAll(tmpDir)

	lm := NewLockManager(tmpDir)
	lm.WriteDraft("# SRS: Test Content")
	lm.Lock()

	// Verify should pass.
	if err := lm.VerifyIntegrity(); err != nil {
		t.Fatalf("integrity check should pass: %v", err)
	}

	// Tamper with the SRS file.
	os.Chmod(lm.SRSPath(), 0644) // Make writable to tamper.
	os.WriteFile(lm.SRSPath(), []byte("# SRS: Tampered"), 0444)

	// Verify should fail.
	if err := lm.VerifyIntegrity(); err == nil {
		t.Error("integrity check should fail after tampering")
	}
}

func TestLockManagerReadSRS(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-srs-read-*")
	defer os.RemoveAll(tmpDir)

	lm := NewLockManager(tmpDir)
	lm.WriteDraft("# SRS: My Project\n\nContent here.")

	content, err := lm.ReadSRS()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(content, "My Project") {
		t.Error("content should contain 'My Project'")
	}
}

// --- Approval Manager tests ---

func TestApprovalFlow(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-srs-approval-*")
	defer os.RemoveAll(tmpDir)

	emitter := events.NewEmitter()
	am := NewApprovalManager(tmpDir, emitter, DelegateUser)

	// Submit a valid SRS.
	formatErrors, err := am.SubmitDraft(validSRS())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(formatErrors) != 0 {
		t.Fatalf("expected no format errors: %v", formatErrors)
	}

	// Should not be approved yet.
	if am.IsApproved() {
		t.Error("should not be approved before Approve()")
	}

	// Approve.
	hash, err := am.Approve("user")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if hash == "" {
		t.Error("expected hash")
	}

	// Should be approved.
	if !am.IsApproved() {
		t.Error("should be approved after Approve()")
	}

	// Approving again should fail.
	_, err = am.Approve("user")
	if err == nil {
		t.Error("expected error approving already-locked SRS")
	}
}

func TestApprovalRejectsInvalidSRS(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-srs-reject-*")
	defer os.RemoveAll(tmpDir)

	am := NewApprovalManager(tmpDir, events.NewEmitter(), DelegateUser)

	// Submit an invalid SRS (missing sections).
	formatErrors, err := am.SubmitDraft("# Not a valid SRS")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(formatErrors) == 0 {
		t.Error("expected format errors for invalid SRS")
	}
}

func TestApprovalDelegation(t *testing.T) {
	am := NewApprovalManager("/tmp/test", events.NewEmitter(), DelegateClaw)
	if !am.IsDelegatedToClaw() {
		t.Error("expected claw delegation")
	}

	am2 := NewApprovalManager("/tmp/test", events.NewEmitter(), DelegateUser)
	if am2.IsDelegatedToClaw() {
		t.Error("expected user delegation")
	}
}

// --- ECO Manager tests ---

func setupECOTest(t *testing.T) (*ECOManager, *state.DB) {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", "axiom-eco-test-*")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.RunMigrations()
	t.Cleanup(func() { db.Close() })

	emitter := events.NewEmitter()
	mgr := NewECOManager(db, emitter, axiomDir)

	return mgr, db
}

func TestProposeECOValidCategory(t *testing.T) {
	mgr, _ := setupECOTest(t)

	eco, err := mgr.ProposeECO("ECO-DEP", "left-pad removed from npm", "FR-001, AC-003", "Use string-pad instead")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if eco.Status != "proposed" {
		t.Errorf("status = %s, want proposed", eco.Status)
	}
	if eco.Category != "Dependency Unavailable" {
		t.Errorf("category = %s", eco.Category)
	}
}

func TestProposeECOInvalidCategory(t *testing.T) {
	mgr, _ := setupECOTest(t)

	_, err := mgr.ProposeECO("ECO-INVALID", "not real", "FR-001", "n/a")
	if err == nil {
		t.Error("expected error for invalid ECO category")
	}
}

func TestAllSixECOCategories(t *testing.T) {
	mgr, _ := setupECOTest(t)

	for code := range ValidECOCategories {
		_, err := mgr.ProposeECO(code, "test", "FR-001", "substitute")
		if err != nil {
			t.Errorf("category %s should be valid: %v", code, err)
		}
	}
}

func TestApproveECO(t *testing.T) {
	mgr, db := setupECOTest(t)

	eco, _ := mgr.ProposeECO("ECO-API", "endpoint changed", "FR-005", "use v2 endpoint")

	if err := mgr.ApproveECO(eco.ID, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Verify status in DB.
	ecos, _ := db.ListECOs("approved")
	if len(ecos) != 1 {
		t.Fatalf("expected 1 approved ECO, got %d", len(ecos))
	}
	if ecos[0].ApprovedBy != "user" {
		t.Errorf("approved_by = %s", ecos[0].ApprovedBy)
	}

	// Verify addendum file was written.
	addendumPath := filepath.Join(mgr.ecoDir, "ECO-001.md")
	if _, err := os.Stat(addendumPath); os.IsNotExist(err) {
		t.Error("ECO addendum file should exist")
	}
}

func TestRejectECO(t *testing.T) {
	mgr, db := setupECOTest(t)

	eco, _ := mgr.ProposeECO("ECO-SEC", "CVE found", "FR-003", "use alternative lib")
	mgr.RejectECO(eco.ID, "user")

	ecos, _ := db.ListECOs("rejected")
	if len(ecos) != 1 {
		t.Fatalf("expected 1 rejected ECO, got %d", len(ecos))
	}
}

func TestCancelAffectedTasks(t *testing.T) {
	mgr, db := setupECOTest(t)

	// Create tasks.
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation", CreatedAt: time.Now()})
	db.CreateTask(&state.Task{ID: "t2", Title: "T2", Status: "queued", Tier: "standard", TaskType: "implementation", CreatedAt: time.Now()})

	eco, _ := mgr.ProposeECO("ECO-DEP", "lib removed", "FR-001", "use alt")
	mgr.ApproveECO(eco.ID, "user")

	if err := mgr.CancelAffectedTasks(eco.ID, []string{"t1", "t2"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Verify tasks are cancelled.
	task1, _ := db.GetTask("t1")
	if task1.Status != "cancelled_eco" {
		t.Errorf("t1 status = %s, want cancelled_eco", task1.Status)
	}
	task2, _ := db.GetTask("t2")
	if task2.Status != "cancelled_eco" {
		t.Errorf("t2 status = %s", task2.Status)
	}
}

func TestValidateECOCategory(t *testing.T) {
	valid := []string{"ECO-DEP", "ECO-API", "ECO-SEC", "ECO-PLT", "ECO-LIC", "ECO-PRV"}
	for _, code := range valid {
		if !ValidateECOCategory(code) {
			t.Errorf("%s should be valid", code)
		}
	}
	invalid := []string{"ECO-INVALID", "DEP", "", "ECO-"}
	for _, code := range invalid {
		if ValidateECOCategory(code) {
			t.Errorf("%s should be invalid", code)
		}
	}
}
