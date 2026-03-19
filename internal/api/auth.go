package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TokenScope defines the access level of a token.
type TokenScope string

const (
	ScopeReadOnly    TokenScope = "read-only"
	ScopeFullControl TokenScope = "full-control"
)

// TokenInfo holds metadata about an API token.
type TokenInfo struct {
	ID        string     `json:"id"`
	Token     string     `json:"-"` // Full token, only shown at creation (not serialized by default)
	Scope     TokenScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Revoked   bool       `json:"revoked"`
}

// persistedToken is the on-disk format for a token. It includes the token
// value so it can be restored on startup.
type persistedToken struct {
	ID        string     `json:"id"`
	Token     string     `json:"token"`
	Scope     TokenScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Revoked   bool       `json:"revoked"`
}

// TokenAuth manages API authentication tokens with optional persistent storage.
// See Architecture Section 24.3.
//
// When a storageDir is configured (via NewTokenAuthWithStorage), tokens are
// persisted to disk at ~/.axiom/api-tokens/ as JSON files. This allows tokens
// to survive engine restarts. When no storageDir is set (via NewTokenAuth),
// tokens are stored only in memory.
type TokenAuth struct {
	mu         sync.RWMutex
	tokens     map[string]*TokenInfo // token value -> info
	storageDir string               // path to persistent token storage (empty = in-memory only)
}

// NewTokenAuth creates a token authentication manager with in-memory storage only.
func NewTokenAuth() *TokenAuth {
	return &TokenAuth{
		tokens: make(map[string]*TokenInfo),
	}
}

// NewTokenAuthWithStorage creates a token authentication manager that persists
// tokens to the given directory (typically ~/.axiom/api-tokens/).
// Existing tokens are loaded from disk on creation.
// See Architecture Section 28.3.
func NewTokenAuthWithStorage(storageDir string) (*TokenAuth, error) {
	ta := &TokenAuth{
		tokens:     make(map[string]*TokenInfo),
		storageDir: storageDir,
	}

	// Create the storage directory if it does not exist.
	if err := os.MkdirAll(storageDir, 0700); err != nil {
		return nil, fmt.Errorf("create token storage dir: %w", err)
	}

	// Load existing tokens from disk.
	if err := ta.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("load tokens: %w", err)
	}

	return ta, nil
}

// Generate creates a new API token with the given scope and expiration.
// Returns the full token string (prefixed with "axm_sk_").
// See Architecture Section 24.3.
func (ta *TokenAuth) Generate(scope TokenScope, expiresIn time.Duration) (*TokenInfo, string) {
	if expiresIn == 0 {
		expiresIn = 24 * time.Hour
	}
	if scope == "" {
		scope = ScopeFullControl
	}

	// Generate random token: axm_sk_<32 hex chars>
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)
	tokenValue := "axm_sk_" + hex.EncodeToString(randomBytes)

	// Token ID is a shorter identifier for listing/revocation.
	idBytes := make([]byte, 4)
	rand.Read(idBytes)
	tokenID := hex.EncodeToString(idBytes)

	now := time.Now()
	info := &TokenInfo{
		ID:        tokenID,
		Token:     tokenValue,
		Scope:     scope,
		CreatedAt: now,
		ExpiresAt: now.Add(expiresIn),
	}

	ta.mu.Lock()
	ta.tokens[tokenValue] = info
	ta.mu.Unlock()

	// Persist to disk if storage is configured.
	if ta.storageDir != "" {
		_ = ta.persistToken(tokenValue, info)
	}

	return info, tokenValue
}

// Validate checks if a token is valid (exists, not expired, not revoked).
// Returns the token info if valid, or an error.
func (ta *TokenAuth) Validate(token string) (*TokenInfo, error) {
	ta.mu.RLock()
	info, exists := ta.tokens[token]
	ta.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid token")
	}
	if info.Revoked {
		return nil, fmt.Errorf("token has been revoked")
	}
	if time.Now().After(info.ExpiresAt) {
		return nil, fmt.Errorf("token has expired")
	}
	return info, nil
}

// Revoke invalidates a token by ID.
func (ta *TokenAuth) Revoke(tokenID string) error {
	ta.mu.Lock()
	defer ta.mu.Unlock()

	for tokenValue, info := range ta.tokens {
		if info.ID == tokenID {
			info.Revoked = true
			// Persist the revocation to disk.
			if ta.storageDir != "" {
				_ = ta.persistToken(tokenValue, info)
			}
			return nil
		}
	}
	return fmt.Errorf("token not found: %s", tokenID)
}

// List returns all non-revoked tokens (without the token values).
func (ta *TokenAuth) List() []*TokenInfo {
	ta.mu.RLock()
	defer ta.mu.RUnlock()

	var result []*TokenInfo
	for _, info := range ta.tokens {
		if !info.Revoked {
			result = append(result, info)
		}
	}
	return result
}

// persistToken writes a token to disk as a JSON file named by token ID.
func (ta *TokenAuth) persistToken(tokenValue string, info *TokenInfo) error {
	pt := persistedToken{
		ID:        info.ID,
		Token:     tokenValue,
		Scope:     info.Scope,
		CreatedAt: info.CreatedAt,
		ExpiresAt: info.ExpiresAt,
		Revoked:   info.Revoked,
	}

	data, err := json.MarshalIndent(pt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	path := filepath.Join(ta.storageDir, info.ID+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

// loadFromDisk reads all token files from the storage directory and loads
// them into memory. Expired and revoked tokens are loaded but will fail
// validation checks normally.
func (ta *TokenAuth) loadFromDisk() error {
	entries, err := os.ReadDir(ta.storageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read token dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(ta.storageDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable files
		}

		var pt persistedToken
		if err := json.Unmarshal(data, &pt); err != nil {
			continue // skip malformed files
		}

		info := &TokenInfo{
			ID:        pt.ID,
			Token:     pt.Token,
			Scope:     pt.Scope,
			CreatedAt: pt.CreatedAt,
			ExpiresAt: pt.ExpiresAt,
			Revoked:   pt.Revoked,
		}
		ta.tokens[pt.Token] = info
	}

	return nil
}
