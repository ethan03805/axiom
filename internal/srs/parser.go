// Package srs implements SRS (Software Requirements Specification) management
// including format validation, approval flow, immutability enforcement, and
// Engineering Change Order (ECO) processing.
//
// See Architecture.md Sections 6 and 7 for the full specification.
package srs

import (
	"fmt"
	"regexp"
	"strings"
)

// Required top-level sections in an SRS document per Architecture Section 6.1.
var requiredSections = []string{
	"## 1. Architecture",
	"## 2. Requirements & Constraints",
	"## 3. Test Strategy",
	"## 4. Acceptance Criteria",
}

// Required subsections within the Architecture section.
var requiredArchSubsections = []string{
	"### 1.1 System Overview",
	"### 1.2 Component Breakdown",
	"### 1.3 Technology Decisions",
	"### 1.4 Data Model",
	"### 1.5 Directory Structure",
}

// Required subsections within the Requirements section.
var requiredReqSubsections = []string{
	"### 2.1 Functional Requirements",
	"### 2.2 Non-Functional Requirements",
	"### 2.3 Constraints",
	"### 2.4 Assumptions",
}

// Patterns for valid requirement IDs.
var (
	frPattern  = regexp.MustCompile(`FR-\d{3}`)
	nfrPattern = regexp.MustCompile(`NFR-\d{3}`)
	acPattern  = regexp.MustCompile(`AC-\d{3}`)
	icPattern  = regexp.MustCompile(`IC-\d{3}`)
)

// ValidateFormat checks that an SRS document follows the mandated structure
// from Architecture Section 6.1. Returns a list of validation errors.
func ValidateFormat(content string) []string {
	var errors []string

	if strings.TrimSpace(content) == "" {
		return []string{"SRS document is empty"}
	}

	// Check for the title line: # SRS: <Project Name>
	if !strings.HasPrefix(strings.TrimSpace(content), "# SRS:") {
		errors = append(errors, "SRS must start with '# SRS: <Project Name>'")
	}

	// Check required top-level sections.
	for _, section := range requiredSections {
		if !strings.Contains(content, section) {
			errors = append(errors, fmt.Sprintf("missing required section: %s", section))
		}
	}

	// Check required Architecture subsections.
	if strings.Contains(content, "## 1. Architecture") {
		for _, sub := range requiredArchSubsections {
			if !strings.Contains(content, sub) {
				errors = append(errors, fmt.Sprintf("missing required subsection: %s", sub))
			}
		}
	}

	// Check required Requirements subsections.
	if strings.Contains(content, "## 2. Requirements & Constraints") {
		for _, sub := range requiredReqSubsections {
			if !strings.Contains(content, sub) {
				errors = append(errors, fmt.Sprintf("missing required subsection: %s", sub))
			}
		}
	}

	// Check for at least one functional requirement.
	if strings.Contains(content, "### 2.1 Functional Requirements") {
		if !frPattern.MatchString(content) {
			errors = append(errors, "no functional requirements found (expected FR-xxx pattern)")
		}
	}

	// Check for acceptance criteria section with at least one criterion.
	if strings.Contains(content, "## 4. Acceptance Criteria") {
		if !acPattern.MatchString(content) && !icPattern.MatchString(content) {
			errors = append(errors, "no acceptance criteria found (expected AC-xxx or IC-xxx pattern)")
		}
	}

	return errors
}

// ExtractRequirementIDs extracts all requirement IDs from an SRS document.
// Returns separate slices for FR, NFR, AC, and IC references.
func ExtractRequirementIDs(content string) (frs, nfrs, acs, ics []string) {
	frs = frPattern.FindAllString(content, -1)
	nfrs = nfrPattern.FindAllString(content, -1)
	acs = acPattern.FindAllString(content, -1)
	ics = icPattern.FindAllString(content, -1)
	return deduplicate(frs), deduplicate(nfrs), deduplicate(acs), deduplicate(ics)
}

// ValidateRequirementRef checks if a reference string is a valid SRS reference.
func ValidateRequirementRef(ref string) bool {
	return frPattern.MatchString(ref) || nfrPattern.MatchString(ref) ||
		acPattern.MatchString(ref) || icPattern.MatchString(ref)
}

func deduplicate(s []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
