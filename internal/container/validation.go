package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
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

	// Wait for the validation container to exit and read results.
	// The container runs the profile commands internally and writes a
	// structured result file to the mounted working directory.
	timeout := time.Duration(vs.config.TimeoutMinutes) * time.Minute
	if timeout == 0 {
		timeout = 10 * time.Minute
	}

	exitReason := "completed"
	result, waitErr := vs.waitForContainerExit(ctx, resp.ID, containerName, workDir, timeout)
	if waitErr != nil {
		exitReason = "error"
		// If we timed out or failed to read results, we still need to
		// clean up the container and return whatever information we have.
		if result == nil {
			result = &pipeline.ValidationResult{}
		}
	}

	// Clean up the container.
	_ = vs.docker.ContainerRemove(ctx, resp.ID, removeOptions(true))
	_ = vs.db.UpdateContainerSessionStopped(containerName, time.Now(), exitReason)

	vs.emitter.Emit(events.Event{
		Type:      events.EventContainerDestroyed,
		TaskID:    taskID,
		AgentType: "validator",
		AgentID:   containerName,
	})

	if waitErr != nil {
		return result, fmt.Errorf("validation container %s: %w", containerName, waitErr)
	}

	return result, nil
}

// overlaySkipDirs lists directories that are skipped during the project copy
// to keep overlay creation fast. These directories are either Axiom-internal,
// version control metadata, or large dependency trees that are mounted
// separately via read-only cache mounts.
var overlaySkipDirs = map[string]bool{
	".axiom":       true,
	".git":         true,
	"node_modules": true,
}

// createOverlay creates a working directory that combines the project state
// with the Meeseeks' staged output. Files from staging override project files.
//
// The overlay approach per Architecture Section 13.3: read-only snapshot of
// project at HEAD (base layer) + writable layer with Meeseeks output applied.
// Since we are running in a Docker container with bind mounts, a directory
// copy with override is sufficient.
//
// Steps:
//  1. Create a temp working directory.
//  2. Copy project directory contents to the working directory (base layer),
//     skipping .axiom/, .git/, and node_modules/ for speed.
//  3. Copy staging directory contents on top (overlay layer), overriding any
//     project files that the Meeseeks modified.
func (vs *ValidationSandbox) createOverlay(taskID, stagingDir, projectDir string) (string, error) {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("axiom-validate-%s-*", taskID))
	if err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}

	// Step 1: Copy project files as the base layer. We use cp -a to preserve
	// permissions and timestamps. The --exclude flags are not portable with
	// cp, so we enumerate top-level entries and skip the excluded directories.
	if err := vs.copyProjectToWorkDir(projectDir, workDir); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("copy project base layer: %w", err)
	}

	// Step 2: Copy staging directory contents on top as the overlay layer.
	// This overrides any project files that the Meeseeks modified.
	if err := vs.copyStagingToWorkDir(stagingDir, workDir); err != nil {
		os.RemoveAll(workDir)
		return "", fmt.Errorf("copy staging overlay layer: %w", err)
	}

	return workDir, nil
}

// copyProjectToWorkDir copies the project directory contents into the working
// directory, skipping directories listed in overlaySkipDirs. Each non-skipped
// top-level entry is copied using `cp -a` via exec.Command.
func (vs *ValidationSandbox) copyProjectToWorkDir(projectDir, workDir string) error {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return fmt.Errorf("read project dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()

		// Skip directories that should not be part of the validation overlay.
		if entry.IsDir() && overlaySkipDirs[name] {
			continue
		}

		src := filepath.Join(projectDir, name)
		dst := filepath.Join(workDir, name)

		// Use cp -a to preserve attributes (permissions, timestamps, symlinks).
		cmd := exec.Command("cp", "-a", src, dst)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cp -a %s %s: %w: %s", src, dst, err, string(output))
		}
	}

	return nil
}

// copyStagingToWorkDir copies the Meeseeks staging directory contents into the
// working directory. Staging files override project files (this is the overlay
// layer). Uses `cp -a` to ensure the full staging tree is applied.
func (vs *ValidationSandbox) copyStagingToWorkDir(stagingDir, workDir string) error {
	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		// An empty or missing staging dir is not an error -- there may simply
		// be no staged files (e.g., a deletion-only manifest).
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read staging dir: %w", err)
	}

	if len(entries) == 0 {
		return nil
	}

	for _, entry := range entries {
		src := filepath.Join(stagingDir, entry.Name())
		dst := filepath.Join(workDir, entry.Name())

		// Use cp -a to preserve attributes and recursively copy.
		cmd := exec.Command("cp", "-a", src, dst)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cp -a %s %s: %w: %s", src, dst, err, string(output))
		}
	}

	return nil
}

// validationResultFile is the well-known filename that validation containers
// write their structured results to inside the mounted working directory.
const validationResultFile = ".axiom-validation-result.json"

// containerPollInterval is the interval between polling checks when waiting
// for a validation container to exit. The DockerClient interface does not
// expose ContainerWait, so we poll ContainerList instead.
const containerPollInterval = 2 * time.Second

// validationResultJSON is the JSON structure that the validation container
// writes to the result file. This mirrors pipeline.ValidationResult but is
// defined separately for serialization/deserialization at the container
// boundary.
type validationResultJSON struct {
	CompilePass   bool     `json:"compile_pass"`
	CompileError  string   `json:"compile_error,omitempty"`
	LintPass      bool     `json:"lint_pass"`
	LintError     string   `json:"lint_error,omitempty"`
	LintWarnings  []string `json:"lint_warnings,omitempty"`
	TestPass      bool     `json:"test_pass"`
	TestError     string   `json:"test_error,omitempty"`
	TestCount     int      `json:"test_count"`
	TestPassed    int      `json:"test_passed"`
	SecurityPass  bool     `json:"security_pass"`
	SecurityError string   `json:"security_error,omitempty"`
}

// waitForContainerExit polls the Docker daemon to determine when the
// validation container has exited, then reads and parses the structured
// result file from the working directory.
//
// Since the DockerClient interface does not include ContainerWait, this
// method polls ContainerList to check if the container is still running.
// It respects the provided timeout; if the container does not exit within
// the timeout, it is killed and an error is returned.
func (vs *ValidationSandbox) waitForContainerExit(
	ctx context.Context,
	containerID string,
	containerName string,
	workDir string,
	timeout time.Duration,
) (*pipeline.ValidationResult, error) {
	deadline := time.Now().Add(timeout)

	// Poll until the container exits or we hit the timeout.
	for {
		if time.Now().After(deadline) {
			// Timeout: kill the container.
			_ = vs.docker.ContainerKill(ctx, containerID, "KILL")
			return nil, fmt.Errorf("validation timed out after %s", timeout)
		}

		running, err := vs.isContainerRunning(ctx, containerID)
		if err != nil {
			return nil, fmt.Errorf("check container status: %w", err)
		}

		if !running {
			break
		}

		// Wait before polling again.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(containerPollInterval):
		}
	}

	// Container has exited. Read the result file.
	return vs.readValidationResult(workDir)
}

// isContainerRunning checks whether the given container ID is still in a
// running state by querying ContainerList. Returns false if the container
// is not found or is in a non-running state.
func (vs *ValidationSandbox) isContainerRunning(ctx context.Context, containerID string) (bool, error) {
	containers, err := vs.docker.ContainerList(ctx, dockercontainer.ListOptions{
		All: true, // Include stopped containers so we can distinguish "exited" from "not found"
	})
	if err != nil {
		return false, fmt.Errorf("list containers: %w", err)
	}

	for _, c := range containers {
		if c.ID == containerID {
			return c.State == "running", nil
		}
	}

	// Container not found in the list -- it has been removed or never existed.
	return false, nil
}

// readValidationResult reads and parses the validation result JSON file from
// the working directory. If the result file does not exist (e.g., the container
// crashed before writing it), a default failing result is returned with an
// appropriate error message.
func (vs *ValidationSandbox) readValidationResult(workDir string) (*pipeline.ValidationResult, error) {
	resultPath := filepath.Join(workDir, validationResultFile)

	data, err := os.ReadFile(resultPath)
	if err != nil {
		if os.IsNotExist(err) {
			// The container exited without writing a result file. This
			// typically means the container crashed or the validation
			// script failed to run. Return a failing result.
			return &pipeline.ValidationResult{
				CompilePass:  false,
				CompileError: "validation container exited without writing results",
				LintPass:     false,
				LintError:    "validation container exited without writing results",
				TestPass:     false,
				TestError:    "validation container exited without writing results",
			}, fmt.Errorf("validation result file not found: container may have crashed")
		}
		return nil, fmt.Errorf("read validation result: %w", err)
	}

	var raw validationResultJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return &pipeline.ValidationResult{
			CompilePass:  false,
			CompileError: "failed to parse validation result JSON",
		}, fmt.Errorf("parse validation result: %w", err)
	}

	return &pipeline.ValidationResult{
		CompilePass:   raw.CompilePass,
		CompileError:  raw.CompileError,
		LintPass:      raw.LintPass,
		LintError:     raw.LintError,
		LintWarnings:  raw.LintWarnings,
		TestPass:      raw.TestPass,
		TestError:     raw.TestError,
		TestCount:     raw.TestCount,
		TestPassed:    raw.TestPassed,
		SecurityPass:  raw.SecurityPass,
		SecurityError: raw.SecurityError,
	}, nil
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
