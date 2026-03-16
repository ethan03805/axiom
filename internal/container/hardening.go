// Package container implements Docker container lifecycle management for the
// Axiom Trusted Engine. All container spawning, destruction, and tracking is
// performed exclusively by the engine -- no LLM agent may directly invoke Docker.
//
// See Architecture.md Section 12 for the full Docker Sandbox Architecture spec.
package container

import (
	"fmt"
	"strconv"
	"strings"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
)

// HardeningPolicy holds all container hardening settings from Architecture
// Section 12.6.1. Every container spawned by the engine applies these flags.
type HardeningPolicy struct {
	ReadOnlyRootfs bool
	CapDrop        []string
	SecurityOpts   []string
	PidsLimit      int64
	TmpfsSize      string // e.g. "256m"
	NetworkMode    string
	UserUID        int
	UserGID        int
	CPULimit       float64 // CPU cores (e.g. 0.5)
	MemoryLimit    int64   // bytes
	SeccompProfile string  // optional path to seccomp JSON profile
}

// DefaultHardening returns the standard container hardening policy as specified
// in Architecture Section 12.6.1. This is applied to all Meeseeks, reviewer,
// and sub-orchestrator containers.
func DefaultHardening(cpuLimit float64, memoryLimit string, uid, gid int) *HardeningPolicy {
	return &HardeningPolicy{
		ReadOnlyRootfs: true,
		CapDrop:        []string{"ALL"},
		SecurityOpts:   []string{"no-new-privileges"},
		PidsLimit:      256,
		TmpfsSize:      "256m",
		NetworkMode:    "none",
		UserUID:        uid,
		UserGID:        gid,
		CPULimit:       cpuLimit,
		MemoryLimit:    ParseMemoryBytes(memoryLimit),
	}
}

// ValidationHardening returns the hardening policy for validation sandbox
// containers. Validation sandboxes have higher resource limits but the same
// security restrictions. See Architecture Section 13.3.
func ValidationHardening(cpuLimit float64, memoryLimit string, uid, gid int) *HardeningPolicy {
	h := DefaultHardening(cpuLimit, memoryLimit, uid, gid)
	// Validation sandboxes get the same hardening but with their own
	// resource limits (typically higher than Meeseeks containers).
	return h
}

// ApplyToHostConfig applies the hardening policy to a Docker HostConfig.
// This produces the HostConfig with all security flags, resource limits,
// and mount configuration as specified in Architecture Section 12.6.1.
func (h *HardeningPolicy) ApplyToHostConfig(mounts []mount.Mount) *dockercontainer.HostConfig {
	pidsLimit := h.PidsLimit

	hostConfig := &dockercontainer.HostConfig{
		ReadonlyRootfs: h.ReadOnlyRootfs,
		SecurityOpt:    h.SecurityOpts,
		CapDrop:        h.CapDrop,
		NetworkMode:    dockercontainer.NetworkMode(h.NetworkMode),
		Resources: dockercontainer.Resources{
			NanoCPUs:  int64(h.CPULimit * 1e9),
			Memory:    h.MemoryLimit,
			PidsLimit: &pidsLimit,
		},
		Tmpfs: map[string]string{
			"/tmp": fmt.Sprintf("rw,noexec,size=%s", h.TmpfsSize),
		},
		Mounts: mounts,
	}

	// Apply optional seccomp profile if configured.
	if h.SeccompProfile != "" {
		hostConfig.SecurityOpt = append(hostConfig.SecurityOpt,
			fmt.Sprintf("seccomp=%s", h.SeccompProfile))
	}

	return hostConfig
}

// UserString returns the "uid:gid" string for Docker's --user flag.
func (h *HardeningPolicy) UserString() string {
	return fmt.Sprintf("%d:%d", h.UserUID, h.UserGID)
}

// ParseMemoryBytes converts a human-readable memory string (e.g. "2g", "512m",
// "1024k") to bytes. Returns 0 if the input is empty or unparseable.
func ParseMemoryBytes(s string) int64 {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(strings.ToLower(s))

	var multiplier int64 = 1
	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}
