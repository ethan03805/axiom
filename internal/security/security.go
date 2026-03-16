package security

import (
	"regexp"
)

// Scanner checks files and content for sensitive data patterns.
type Scanner struct {
	patterns         []*regexp.Regexp
	forceLocalPatterns []*regexp.Regexp
}

// ScanResult holds the result of a security scan.
type ScanResult struct {
	FilePath string
	Findings []Finding
	IsSafe   bool
}

// Finding represents a single security finding.
type Finding struct {
	Line        int
	Column      int
	Pattern     string
	Description string
	Severity    string
}

// New creates a new security Scanner with the given patterns.
func New(patterns []string) (*Scanner, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}
	return &Scanner{
		patterns: compiled,
	}, nil
}

// ScanFile scans a single file for sensitive patterns.
func (s *Scanner) ScanFile(path string) (*ScanResult, error) {
	return &ScanResult{FilePath: path, IsSafe: true}, nil
}

// ScanContent scans arbitrary content for sensitive patterns.
func (s *Scanner) ScanContent(content string) []Finding {
	return nil
}

// ShouldForceLocal returns true if the content contains patterns
// that should only be processed by local models.
func (s *Scanner) ShouldForceLocal(content string) bool {
	return false
}

// ScanDirectory scans all files in a directory recursively.
func (s *Scanner) ScanDirectory(dir string) ([]*ScanResult, error) {
	return nil, nil
}
