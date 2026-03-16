// Package security implements secret scanning, prompt injection mitigation,
// and file safety rules for the Axiom Trusted Engine.
//
// See Architecture.md Section 29 for the full Security Model.
package security

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// SecretPattern defines a regex pattern for detecting secrets in content.
type SecretPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// Default secret patterns per Architecture Section 29.4.
var DefaultSecretPatterns = []SecretPattern{
	{Name: "OpenAI API Key", Pattern: regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)},
	{Name: "Axiom API Token", Pattern: regexp.MustCompile(`axm_sk_[a-f0-9]{32}`)},
	{Name: "GitHub Token", Pattern: regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`)},
	{Name: "AWS Access Key", Pattern: regexp.MustCompile(`AKIA[A-Z0-9]{16}`)},
	{Name: "Private Key Block", Pattern: regexp.MustCompile(`-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----`)},
	{Name: "Connection String", Pattern: regexp.MustCompile(`(?i)(postgres|mysql|mongodb|redis)://[^\s"']+:[^\s"']+@`)},
	{Name: "Generic API Key Assignment", Pattern: regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|auth[_-]?token)\s*[:=]\s*["']?[a-zA-Z0-9/+=]{20,}`)},
}

// Default sensitive file path patterns per Architecture Section 29.4.
var DefaultSensitivePathPatterns = []string{
	"*.env*",
	"*.env",
	".env.local",
	".env.production",
	"*credentials*",
	"*secret*",
	"**/secrets/**",
}

// SecretScanner scans content for secrets and sensitive file patterns.
// See Architecture Section 29.4.
type SecretScanner struct {
	patterns      []SecretPattern
	pathPatterns  []string
}

// NewSecretScanner creates a scanner with default patterns plus any custom ones.
func NewSecretScanner(customPathPatterns []string) *SecretScanner {
	pathPatterns := append([]string{}, DefaultSensitivePathPatterns...)
	pathPatterns = append(pathPatterns, customPathPatterns...)

	return &SecretScanner{
		patterns:     DefaultSecretPatterns,
		pathPatterns: pathPatterns,
	}
}

// Finding represents a detected secret in content.
type Finding struct {
	Line        int    // 1-based line number
	PatternName string // Which pattern matched
	Redacted    string // The redacted replacement text
}

// ScanContent scans text content for secrets. Returns findings and the
// redacted version of the content with secrets replaced by [REDACTED].
// See Architecture Section 29.4 point 3 (Redaction Policy).
func (s *SecretScanner) ScanContent(content string) ([]Finding, string) {
	var findings []Finding
	redacted := content
	lines := strings.Split(content, "\n")

	for _, pattern := range s.patterns {
		for lineNum, line := range lines {
			if pattern.Pattern.MatchString(line) {
				findings = append(findings, Finding{
					Line:        lineNum + 1,
					PatternName: pattern.Name,
					Redacted:    "[REDACTED]",
				})
			}
		}
		// Apply redaction to the full content.
		redacted = pattern.Pattern.ReplaceAllString(redacted, "[REDACTED]")
	}

	return findings, redacted
}

// IsSensitivePath checks if a file path matches any sensitive pattern.
// See Architecture Section 29.4 point 1.
func (s *SecretScanner) IsSensitivePath(filePath string) bool {
	base := filepath.Base(filePath)
	normalized := filepath.ToSlash(filePath)

	for _, pattern := range s.pathPatterns {
		// Check against basename.
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
		// Check against full path.
		if matched, _ := filepath.Match(pattern, normalized); matched {
			return true
		}
		// Simple substring checks for patterns like "*credentials*".
		clean := strings.Trim(pattern, "*")
		if clean != "" && strings.Contains(normalized, clean) {
			return true
		}
	}
	return false
}

// ShouldForceLocal returns true if the content or file path indicates that
// inference should be routed to local BitNet only.
// See Architecture Section 29.4 point 4.
func (s *SecretScanner) ShouldForceLocal(filePath, content string) bool {
	if s.IsSensitivePath(filePath) {
		return true
	}
	findings, _ := s.ScanContent(content)
	return len(findings) > 0
}

// SecretDensity returns the number of secret findings per line.
// High density files may be excluded entirely from context.
func (s *SecretScanner) SecretDensity(content string) float64 {
	findings, _ := s.ScanContent(content)
	lines := strings.Count(content, "\n") + 1
	if lines == 0 {
		return 0
	}
	return float64(len(findings)) / float64(lines)
}

// RedactForPromptLog applies redaction to prompt log content.
// See Architecture Section 29.4 point 5: "Prompt logs SHALL NEVER store
// detected secrets in raw form."
func (s *SecretScanner) RedactForPromptLog(content string) string {
	_, redacted := s.ScanContent(content)
	return redacted
}

// FormatRedactionLog creates an audit-safe log entry for a redaction event.
// Logs the file, line, and pattern but NOT the secret value.
// See Architecture Section 29.4 point 3.
func FormatRedactionLog(filePath string, finding Finding) string {
	return fmt.Sprintf("redaction: file=%s line=%d pattern=%s", filePath, finding.Line, finding.PatternName)
}
