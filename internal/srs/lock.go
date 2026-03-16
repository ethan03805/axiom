package srs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// LockManager handles SRS immutability: writing the SRS file with read-only
// permissions, computing and verifying SHA-256 integrity hashes, and
// preventing modification after approval.
//
// See Architecture Section 6.2.
type LockManager struct {
	axiomDir string // Path to .axiom/ directory
}

// NewLockManager creates a LockManager for the given .axiom directory.
func NewLockManager(axiomDir string) *LockManager {
	return &LockManager{axiomDir: axiomDir}
}

// SRSPath returns the path to the SRS file.
func (lm *LockManager) SRSPath() string {
	return filepath.Join(lm.axiomDir, "srs.md")
}

// HashPath returns the path to the SRS hash file.
func (lm *LockManager) HashPath() string {
	return filepath.Join(lm.axiomDir, "srs.md.sha256")
}

// WriteDraft writes the SRS content as a draft (writable).
// Called during the SRS approval loop when the orchestrator submits a draft.
func (lm *LockManager) WriteDraft(content string) error {
	if err := os.MkdirAll(lm.axiomDir, 0755); err != nil {
		return fmt.Errorf("create axiom dir: %w", err)
	}
	if err := os.WriteFile(lm.SRSPath(), []byte(content), 0644); err != nil {
		return fmt.Errorf("write SRS draft: %w", err)
	}
	return nil
}

// Lock makes the SRS immutable by setting read-only file permissions
// and writing the SHA-256 hash file.
// See Architecture Section 6.2: "set `.axiom/srs.md` to read-only file
// permissions" and "SHA-256 hash SHALL be stored."
func (lm *LockManager) Lock() (string, error) {
	content, err := os.ReadFile(lm.SRSPath())
	if err != nil {
		return "", fmt.Errorf("read SRS for locking: %w", err)
	}

	// Compute SHA-256 hash.
	hash := computeHash(content)

	// Write the hash file.
	if err := os.WriteFile(lm.HashPath(), []byte(hash), 0444); err != nil {
		return "", fmt.Errorf("write SRS hash: %w", err)
	}

	// Set the SRS file to read-only.
	if err := os.Chmod(lm.SRSPath(), 0444); err != nil {
		return "", fmt.Errorf("set SRS read-only: %w", err)
	}

	return hash, nil
}

// VerifyIntegrity checks that the SRS file has not been modified since locking
// by comparing its current SHA-256 hash against the stored hash.
// Called on engine startup per Architecture Section 22.3 step 5.
func (lm *LockManager) VerifyIntegrity() error {
	// Check if SRS exists.
	if _, err := os.Stat(lm.SRSPath()); os.IsNotExist(err) {
		return nil // No SRS yet; nothing to verify.
	}

	// Check if hash file exists.
	if _, err := os.Stat(lm.HashPath()); os.IsNotExist(err) {
		return nil // SRS exists but no hash; it may be a draft.
	}

	// Read current SRS content and compute hash.
	content, err := os.ReadFile(lm.SRSPath())
	if err != nil {
		return fmt.Errorf("read SRS for verification: %w", err)
	}
	currentHash := computeHash(content)

	// Read stored hash.
	storedHashBytes, err := os.ReadFile(lm.HashPath())
	if err != nil {
		return fmt.Errorf("read stored hash: %w", err)
	}
	storedHash := string(storedHashBytes)

	if currentHash != storedHash {
		return fmt.Errorf("SRS integrity check failed: hash mismatch (expected %s, got %s). "+
			"The SRS file may have been tampered with", storedHash[:16], currentHash[:16])
	}

	return nil
}

// IsLocked returns true if the SRS has been approved and locked.
func (lm *LockManager) IsLocked() bool {
	info, err := os.Stat(lm.SRSPath())
	if err != nil {
		return false
	}
	// Check if file is read-only (no write bits).
	return info.Mode().Perm()&0222 == 0
}

// ReadSRS reads the current SRS content (draft or locked).
func (lm *LockManager) ReadSRS() (string, error) {
	content, err := os.ReadFile(lm.SRSPath())
	if err != nil {
		return "", fmt.Errorf("read SRS: %w", err)
	}
	return string(content), nil
}

// Unlock temporarily makes the SRS writable (for adding ECO addendums).
// The SRS must be re-locked after modification.
func (lm *LockManager) Unlock() error {
	return os.Chmod(lm.SRSPath(), 0644)
}

func computeHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
