package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
	Token     string     `json:"-"` // Full token, only shown at creation
	Scope     TokenScope `json:"scope"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Revoked   bool       `json:"revoked"`
}

// TokenAuth manages API authentication tokens.
// See Architecture Section 24.3.
type TokenAuth struct {
	mu     sync.RWMutex
	tokens map[string]*TokenInfo // token value -> info
}

// NewTokenAuth creates a token authentication manager.
func NewTokenAuth() *TokenAuth {
	return &TokenAuth{
		tokens: make(map[string]*TokenInfo),
	}
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

	for _, info := range ta.tokens {
		if info.ID == tokenID {
			info.Revoked = true
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
