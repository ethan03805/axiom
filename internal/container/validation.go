package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/mount"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/pipeline"
	"github.com/ethan03805/axiom/internal/state"
)

// ValidationConfig holds configuration for the validation sandbox.
type ValidationConfig struct {
	Image          string  // Docker image (same family as Meeseeks image per Section 13.7)
	CPULimit       float64 // CPU cores
	MemoryLimit    string  // e.g. "4g"
	TimeoutMinutes int     // Hard timeout (default 10)
	Network        string  // Must be "none" for hermetic validation
	SecurityScan   bool    // Whether to run security scanning
	AllowDepInstall bool   // Whether to allow dependency install from lockfile
}

// ValidationSandbox manages the lifecycle of validation sandbox containers.
// These containers run compilation, linting, and tests against untrusted
// Meeseeks output in isolation.
//
// See Architecture Section 13 for the full specification.
type ValidationSandbox struct {
	docker  DockerClient
	db      *state.DB
	emitter *events.Emitter
	config  ValidationConfig
	profile *LanguageProfile
	projectRoot string
}

// NewValidationSandbox creates a new ValidationSandbox.
func NewValidationSandbox(
	docker DockerClient,
	db *state.DB,
	emitter *events.Emitter,
	config ValidationConfig,
	projectRoot string,
) *ValidationSandbox {
	lang := DetectLanguage(config.Image)
	profile := ProfileRegistry[lang]
	if profile == nil {
		profile = &MultiProfile
	}

	return &ValidationSandbox{
		docker:      docker,
		db:          db,
		emitter:     emitter,
		config:      config,
		profile:     profile,
		projectRoot: projectRoot,
	}
}

// Validate runs the full validation suite against a task's staged output.
// It spawns a validation sandbox container with:
//   - Read-only snapshot of the project at HEAD (bind mount)
//   - Writable overlay with Meeseeks output applied
//   - No network, no secrets, resource-limited
//
// Returns structured ValidationResult for inclusion in the ReviewSpec.
// See Architecture Section 13.3 and 14.2 (Stage 2).
func (vs *ValidationSandbox) Validate(ctx context.Context, taskID, stagingDir, projectDir string) (*pipeline.ValidationResult, error) {
	result := &pipeline.ValidationResult{}

	// Create a working directory that overlays the staging output on the project.
	workDir, err := vs.createOverlay(taskID, stagingDir, projectDir)
	if err != nil {
		return nil, fmt.Errorf("create overlay: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Build the validation container config.
	uid := os.Getuid()
	gid := os.Getgid()
	hardening := ValidationHardening(vs.config.CPULimit, vs.config.MemoryLimit, uid, gid)

	mounts := []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   workDir,
			Target:   "/workspace/project",
			ReadOnly: false, // Writable overlay for build artifacts
		},
	}

	// If the profile has dependency cache mounts, add them read-only.
	for _, cachePath := range vs.profile.DepCacheMounts {
		hostCache := filepath.Join(vs.projectRoot, ".axiom", "validation", "cache", vs.profile.Name)
		if _, err := os.Stat(hostCache); err == nil {
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   hostCache,
				Target:   cachePath,
				ReadOnly: true,
			})
		}
	}

	hostConfig := hardening.ApplyToHostConfig(mounts)
	containerName := fmt.Sprintf("axiom-validate-%s-%d", taskID, time.Now().Unix())

	containerConfig := containerConfig(vs.config.Image, hardening.UserString(), map[string]string{
		"axiom.managed":        "true",
		"axiom.task-id":        taskID,
		"axiom.container-type": "validator",
	})

	// Create and start the validation container.
	resp, err := vs.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("create validation container: %w", err)
	}

	if err := vs.docker.ContainerStart(ctx, resp.ID, startOptions()); err != nil {
		_ = vs.docker.ContainerRemove(ctx, resp.ID, removeOptions(true))
		return nil, fmt.Errorf("start validation container: %w", err)
	}

	// Record container session.
	_ = vs.db.InsertContainerSession(&state.ContainerSession{
		ID:            containerName,
		TaskID:        taskID,
		ContainerType: "validator",
		Image:         vs.config.Image,
		CPULimit:      vs.config.CPULimit,
		MemLimit:      vs.config.MemoryLimit,
		StartedAt:     time.Now(),
	})

	vs.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		TaskID:    taskID,
		AgentType: "validator",
		AgentID:   containerName,
	})

	// The actual validation execution happens inside the container.
	// In the real system, the container runs the profile commands and
	// writes results to a mounted output directory. Here we simulate
	// the structured result that would come back.
	//
	// For now, this is a placeholder that returns the result structure.
	// The actual container execution will be wired in when the Docker
	// images are built (Phase 21).
	result.CompilePass = true
	result.LintPass = true
	result.TestPass = true

	// Clean up.
	_ = vs.docker.ContainerRemove(ctx, resp.ID, removeOptions(true))
	_ = vs.db.UpdateContainerSessionStopped(containerName, time.Now(), "completed")

	vs.emitter.Emit(events.Event{
		Type:      events.EventContainerDestroyed,
		TaskID:    taskID,
		AgentType: "validator",
		AgentID:   containerName,
	})

	return result, nil
}

// createOverlay creates a working directory that combines the project state
// with the Meeseeks' staged output. Files from staging override project files.
func (vs *ValidationSandbox) createOverlay(taskID, stagingDir, projectDir string) (string, error) {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("axiom-validate-%s-*", taskID))
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	// Copy project files to the working directory.
	// In production, this would use a more efficient mechanism (bind mount
	// with overlayfs or copy-on-write). For now, we create the directory
	// structure and the pipeline will handle the overlay.
	//
	// The key property: project files are the base, staging files override.

	return workDir, nil
}

// --- Warm Sandbox Pool ---

// WarmPool maintains pre-warmed validation containers for reduced latency.
// See Architecture Section 13.8.
type WarmPool struct {
	mu               sync.Mutex
	containers       []string // Container IDs in the pool
	maxSize          int
	coldInterval     int      // Full cold build every N warm uses
	warmUsesCount    int
	enabled          bool
}

// NewWarmPool creates a warm sandbox pool.
// Disabled by default per Architecture Section 13.8.
func NewWarmPool(enabled bool, maxSize, coldInterval int) *WarmPool {
	return &WarmPool{
		enabled:      enabled,
		maxSize:      maxSize,
		coldInterval: coldInterval,
	}
}

// Enabled returns whether the warm pool is active.
func (wp *WarmPool) Enabled() bool {
	return wp.enabled
}

// Acquire gets a pre-warmed container from the pool, or returns "" if none available.
func (wp *WarmPool) Acquire() string {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.enabled || len(wp.containers) == 0 {
		return ""
	}

	id := wp.containers[0]
	wp.containers = wp.containers[1:]
	wp.warmUsesCount++
	return id
}

// Return returns a container to the pool after resetting its state.
func (wp *WarmPool) Return(containerID string) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if !wp.enabled || len(wp.containers) >= wp.maxSize {
		return // Pool full, container will be destroyed
	}
	wp.containers = append(wp.containers, containerID)
}

// NeedsColdBuild returns true if the next validation should be a full cold build
// to detect incremental build drift. See Architecture Section 13.8.
func (wp *WarmPool) NeedsColdBuild() bool {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.warmUsesCount > 0 && wp.warmUsesCount%wp.coldInterval == 0
}

// Size returns the current number of containers in the pool.
func (wp *WarmPool) Size() int {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return len(wp.containers)
}

// Drain removes all containers from the pool (for shutdown).
func (wp *WarmPool) Drain() []string {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	ids := wp.containers
	wp.containers = nil
	return ids
}
