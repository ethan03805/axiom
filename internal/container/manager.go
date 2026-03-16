package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// ContainerType identifies the purpose of a container in the Axiom architecture.
// See Architecture Section 3.2 for the component summary.
type ContainerType string

const (
	TypeMeeseeks       ContainerType = "meeseeks"
	TypeReviewer       ContainerType = "reviewer"
	TypeSubOrchestrator ContainerType = "sub_orchestrator"
	TypeValidator      ContainerType = "validator"
)

// containerNamePrefix is the prefix for all Axiom-managed containers.
// Used for orphan cleanup and listing. See Architecture Section 12.6.
const containerNamePrefix = "axiom-"

// DockerClient defines the subset of Docker API operations used by the Manager.
// This interface enables unit testing without a real Docker daemon.
type DockerClient interface {
	ContainerCreate(ctx context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig interface{}, platform interface{}, containerName string) (dockercontainer.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options dockercontainer.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options dockercontainer.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options dockercontainer.RemoveOptions) error
	ContainerList(ctx context.Context, options dockercontainer.ListOptions) ([]DockerContainerSummary, error)
	ContainerKill(ctx context.Context, containerID, signal string) error
}

// DockerContainerSummary holds the fields we need from a container list entry.
type DockerContainerSummary struct {
	ID     string
	Names  []string
	Image  string
	State  string
	Labels map[string]string
}

// realDockerClient wraps the actual Docker SDK client, implementing DockerClient.
type realDockerClient struct {
	cli *client.Client
}

// NewDockerClient creates a new Docker client connected to the local daemon.
func NewDockerClient() (*realDockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return &realDockerClient{cli: cli}, nil
}

func (r *realDockerClient) ContainerCreate(ctx context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig interface{}, platform interface{}, containerName string) (dockercontainer.CreateResponse, error) {
	return r.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
}

func (r *realDockerClient) ContainerStart(ctx context.Context, containerID string, options dockercontainer.StartOptions) error {
	return r.cli.ContainerStart(ctx, containerID, options)
}

func (r *realDockerClient) ContainerStop(ctx context.Context, containerID string, options dockercontainer.StopOptions) error {
	return r.cli.ContainerStop(ctx, containerID, options)
}

func (r *realDockerClient) ContainerRemove(ctx context.Context, containerID string, options dockercontainer.RemoveOptions) error {
	return r.cli.ContainerRemove(ctx, containerID, options)
}

func (r *realDockerClient) ContainerList(ctx context.Context, options dockercontainer.ListOptions) ([]DockerContainerSummary, error) {
	containers, err := r.cli.ContainerList(ctx, options)
	if err != nil {
		return nil, err
	}
	var result []DockerContainerSummary
	for _, c := range containers {
		result = append(result, DockerContainerSummary{
			ID:     c.ID,
			Names:  c.Names,
			Image:  c.Image,
			State:  c.State,
			Labels: c.Labels,
		})
	}
	return result, nil
}

func (r *realDockerClient) ContainerKill(ctx context.Context, containerID, signal string) error {
	return r.cli.ContainerKill(ctx, containerID, signal)
}

// Close closes the underlying Docker client connection.
func (r *realDockerClient) Close() error {
	return r.cli.Close()
}

// SpawnRequest contains the parameters needed to spawn a container.
// The Manager uses this to build Docker container configuration.
type SpawnRequest struct {
	TaskID        string
	ContainerType ContainerType
	Image         string        // Docker image to use
	ModelID       string        // Model ID for inference tracking
	CPULimit      float64       // CPU cores (e.g. 0.5)
	MemoryLimit   string        // Memory limit (e.g. "2g")
	TimeoutMin    int           // Hard timeout in minutes
}

// SpawnResult is returned after a container is successfully spawned.
type SpawnResult struct {
	ContainerID   string // Docker container ID
	ContainerName string // Human-readable name (axiom-<task-id>-<timestamp>)
}

// ManagerConfig holds configuration for the container Manager.
type ManagerConfig struct {
	DefaultImage   string  // Default Docker image if not specified in SpawnRequest
	DefaultCPU     float64 // Default CPU limit (cores)
	DefaultMemory  string  // Default memory limit (e.g. "2g")
	DefaultTimeout int     // Default timeout in minutes
	MaxMeeseeks    int     // Maximum concurrent Meeseeks containers
	ProjectRoot    string  // Absolute path to the project root directory
}

// Manager handles the lifecycle of all Docker containers in the Axiom
// Untrusted Agent Plane. It is the sole component authorized to spawn,
// track, and destroy containers. See Architecture Section 12.6.
//
// The Manager enforces:
// - Container naming convention (axiom-<task-id>-<timestamp>)
// - Volume mount setup (specs, staging, IPC directories)
// - Full hardening policy (Section 12.6.1)
// - Hard timeout per container
// - Concurrency limits
// - Orphan cleanup on startup
// - Lifecycle event logging
type Manager struct {
	docker  DockerClient
	db      *state.DB
	emitter *events.Emitter
	config  ManagerConfig

	mu       sync.Mutex
	active   map[string]*trackedContainer // containerName -> tracked info
	timeouts map[string]context.CancelFunc // containerName -> timeout cancel
}

// trackedContainer holds runtime info about a spawned container.
type trackedContainer struct {
	ContainerID   string
	ContainerName string
	TaskID        string
	ContainerType ContainerType
	SpawnedAt     time.Time
}

// NewManager creates a new container Manager. The Docker client, state DB,
// and event emitter are injected for testability.
func NewManager(docker DockerClient, db *state.DB, emitter *events.Emitter, config ManagerConfig) *Manager {
	return &Manager{
		docker:   docker,
		db:       db,
		emitter:  emitter,
		config:   config,
		active:   make(map[string]*trackedContainer),
		timeouts: make(map[string]context.CancelFunc),
	}
}

// SpawnMeeseeks creates and starts a Meeseeks container for the given task.
// The container receives its TaskSpec via the mounted spec directory and
// communicates with the engine via filesystem IPC.
// See Architecture Section 10.5 for the Meeseeks lifecycle.
func (m *Manager) SpawnMeeseeks(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	req.ContainerType = TypeMeeseeks
	return m.spawn(ctx, req)
}

// SpawnReviewer creates and starts a reviewer container for the given task.
// The container receives a ReviewSpec and returns a verdict via IPC.
// See Architecture Section 11 for the reviewer role.
func (m *Manager) SpawnReviewer(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	req.ContainerType = TypeReviewer
	return m.spawn(ctx, req)
}

// SpawnSubOrchestrator creates and starts a sub-orchestrator container.
// See Architecture Section 9 for sub-orchestrator capabilities.
func (m *Manager) SpawnSubOrchestrator(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	req.ContainerType = TypeSubOrchestrator
	return m.spawn(ctx, req)
}

// SpawnValidator creates and starts a validation sandbox container.
// Validation sandboxes have different resource limits and mount configuration
// than agent containers. See Architecture Section 13 for details.
func (m *Manager) SpawnValidator(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	req.ContainerType = TypeValidator
	return m.spawn(ctx, req)
}

// spawn is the core container creation method. It applies defaults, checks
// concurrency limits, creates host directories, builds Docker config with
// full hardening, spawns the container, records the session, and starts
// the timeout monitor.
func (m *Manager) spawn(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	// Apply defaults for any unset fields.
	if req.Image == "" {
		req.Image = m.config.DefaultImage
	}
	if req.CPULimit == 0 {
		req.CPULimit = m.config.DefaultCPU
	}
	if req.MemoryLimit == "" {
		req.MemoryLimit = m.config.DefaultMemory
	}
	if req.TimeoutMin == 0 {
		req.TimeoutMin = m.config.DefaultTimeout
	}

	// Check concurrency limit for non-validator containers.
	// Validators are not counted against the Meeseeks concurrency limit.
	// See Architecture Section 5.2.
	if req.ContainerType != TypeValidator {
		m.mu.Lock()
		agentCount := 0
		for _, tc := range m.active {
			if tc.ContainerType != TypeValidator {
				agentCount++
			}
		}
		if agentCount >= m.config.MaxMeeseeks {
			m.mu.Unlock()
			return nil, fmt.Errorf("concurrency limit reached: %d/%d active agent containers", agentCount, m.config.MaxMeeseeks)
		}
		m.mu.Unlock()
	}

	// Generate container name: axiom-<task-id>-<timestamp>
	// See Architecture Section 12.6.
	timestamp := time.Now().Unix()
	containerName := fmt.Sprintf("%s%s-%d", containerNamePrefix, req.TaskID, timestamp)

	// Create host directories for volume mounts.
	// See Architecture Section 12.3.
	specDir, stagingDir, ipcDir, err := m.setupDirectories(req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("setup directories: %w", err)
	}

	// Build volume mounts per Architecture Section 12.3.
	mounts := []mount.Mount{
		{
			Type:     mount.TypeBind,
			Source:   specDir,
			Target:   "/workspace/spec",
			ReadOnly: true,
		},
		{
			Type:     mount.TypeBind,
			Source:   stagingDir,
			Target:   "/workspace/staging",
			ReadOnly: false,
		},
		{
			Type:     mount.TypeBind,
			Source:   ipcDir,
			Target:   "/workspace/ipc",
			ReadOnly: false,
		},
	}

	// Build hardening policy per Architecture Section 12.6.1.
	uid := os.Getuid()
	gid := os.Getgid()
	hardening := DefaultHardening(req.CPULimit, req.MemoryLimit, uid, gid)

	// Apply hardening to Docker host config.
	hostConfig := hardening.ApplyToHostConfig(mounts)

	// Build container config.
	containerConfig := &dockercontainer.Config{
		Image: req.Image,
		User:  hardening.UserString(),
		Labels: map[string]string{
			"axiom.managed":        "true",
			"axiom.task-id":        req.TaskID,
			"axiom.container-type": string(req.ContainerType),
		},
	}

	// Create the container.
	resp, err := m.docker.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("docker create container %s: %w", containerName, err)
	}

	// Start the container.
	if err := m.docker.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		// Clean up: remove the created but unstarted container.
		_ = m.docker.ContainerRemove(ctx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		return nil, fmt.Errorf("docker start container %s: %w", containerName, err)
	}

	now := time.Now()

	// Track the container.
	m.mu.Lock()
	m.active[containerName] = &trackedContainer{
		ContainerID:   resp.ID,
		ContainerName: containerName,
		TaskID:        req.TaskID,
		ContainerType: req.ContainerType,
		SpawnedAt:     now,
	}
	m.mu.Unlock()

	// Record the container session in SQLite.
	if err := m.db.InsertContainerSession(&state.ContainerSession{
		ID:            containerName,
		TaskID:        req.TaskID,
		ContainerType: string(req.ContainerType),
		Image:         req.Image,
		ModelID:       req.ModelID,
		CPULimit:      req.CPULimit,
		MemLimit:      req.MemoryLimit,
		StartedAt:     now,
	}); err != nil {
		// Log the error but don't fail the spawn -- the container is running.
		m.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: req.TaskID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("failed to record container session: %v", err),
			},
		})
	}

	// Emit container_spawned event.
	m.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		TaskID:    req.TaskID,
		AgentType: string(req.ContainerType),
		AgentID:   containerName,
		Details: map[string]interface{}{
			"container_id":   resp.ID,
			"container_name": containerName,
			"image":          req.Image,
			"model_id":       req.ModelID,
			"cpu_limit":      req.CPULimit,
			"memory_limit":   req.MemoryLimit,
		},
	})

	// Start timeout monitor goroutine.
	// See Architecture Section 12.6: enforce hard timeout per container.
	m.startTimeoutMonitor(containerName, resp.ID, req.TaskID, req.ContainerType, req.TimeoutMin)

	return &SpawnResult{
		ContainerID:   resp.ID,
		ContainerName: containerName,
	}, nil
}

// Destroy force-kills and removes a container, cleans up tracking, and
// records the stop event. This is called when a task completes, fails
// after retry exhaustion, or times out.
// See Architecture Section 10.5: containers are destroyed immediately.
func (m *Manager) Destroy(ctx context.Context, containerName string) error {
	m.mu.Lock()
	tracked, exists := m.active[containerName]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("container not tracked: %s", containerName)
	}

	// Cancel the timeout monitor.
	if cancel, ok := m.timeouts[containerName]; ok {
		cancel()
		delete(m.timeouts, containerName)
	}
	delete(m.active, containerName)
	m.mu.Unlock()

	// Force-remove the container (kills if still running).
	err := m.docker.ContainerRemove(ctx, tracked.ContainerID, dockercontainer.RemoveOptions{Force: true})
	if err != nil {
		// Container may have already exited and been removed. Log but don't fail.
		m.emitter.Emit(events.Event{
			Type:   events.EventContainerDestroyed,
			TaskID: tracked.TaskID,
			Details: map[string]interface{}{
				"container_name": containerName,
				"warning":        fmt.Sprintf("remove returned error: %v", err),
			},
		})
	}

	// Record the stop in SQLite.
	now := time.Now()
	_ = m.db.UpdateContainerSessionStopped(containerName, now, "completed")

	// Emit container_destroyed event.
	m.emitter.Emit(events.Event{
		Type:      events.EventContainerDestroyed,
		TaskID:    tracked.TaskID,
		AgentType: string(tracked.ContainerType),
		AgentID:   containerName,
		Details: map[string]interface{}{
			"container_name": containerName,
			"exit_reason":    "completed",
		},
	})

	return nil
}

// ListActive returns information about all currently tracked containers.
func (m *Manager) ListActive() []*trackedContainer {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*trackedContainer, 0, len(m.active))
	for _, tc := range m.active {
		result = append(result, tc)
	}
	return result
}

// ActiveCount returns the number of currently active agent containers
// (excludes validators). This reflects the count against the concurrency limit.
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, tc := range m.active {
		if tc.ContainerType != TypeValidator {
			count++
		}
	}
	return count
}

// CleanupOrphans finds and destroys any axiom-* containers left over from
// a previous crashed session. Called during engine startup.
// See Architecture Section 12.6 and Section 22.3.
func (m *Manager) CleanupOrphans(ctx context.Context) (int, error) {
	// List all containers (including stopped) with the axiom- name prefix.
	containers, err := m.docker.ContainerList(ctx, dockercontainer.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("name", containerNamePrefix),
		),
	})
	if err != nil {
		return 0, fmt.Errorf("list orphaned containers: %w", err)
	}

	cleaned := 0
	for _, c := range containers {
		// Force-remove each orphaned container.
		if err := m.docker.ContainerRemove(ctx, c.ID, dockercontainer.RemoveOptions{Force: true}); err != nil {
			// Log warning but continue cleaning up others.
			m.emitter.Emit(events.Event{
				Type: events.EventContainerDestroyed,
				Details: map[string]interface{}{
					"orphan_id": c.ID,
					"warning":   fmt.Sprintf("failed to remove orphan: %v", err),
				},
			})
			continue
		}
		cleaned++
	}

	// Mark all active container sessions in SQLite as orphaned.
	orphanedCount, err := m.db.MarkOrphanedContainerSessions()
	if err != nil {
		return cleaned, fmt.Errorf("mark orphaned sessions: %w", err)
	}

	if cleaned > 0 || orphanedCount > 0 {
		m.emitter.Emit(events.Event{
			Type:      events.EventCrashRecovery,
			AgentType: "engine",
			Details: map[string]interface{}{
				"orphan_containers_removed":  cleaned,
				"orphan_sessions_reconciled": orphanedCount,
			},
		})
	}

	return cleaned, nil
}

// setupDirectories creates the host directories needed for container volume
// mounts. Returns absolute paths to spec, staging, and IPC directories.
// See Architecture Section 12.3 for the mount layout.
func (m *Manager) setupDirectories(taskID string) (specDir, stagingDir, ipcDir string, err error) {
	base := filepath.Join(m.config.ProjectRoot, ".axiom", "containers")

	specDir = filepath.Join(base, "specs", taskID)
	stagingDir = filepath.Join(base, "staging", taskID)
	ipcDir = filepath.Join(base, "ipc", taskID)

	// Create spec directory.
	if err := os.MkdirAll(specDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("create spec dir: %w", err)
	}

	// Create staging directory.
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", "", "", fmt.Errorf("create staging dir: %w", err)
	}

	// Create IPC input and output directories.
	// Engine writes to input/, container writes to output/.
	// See Architecture Section 20.3.
	if err := os.MkdirAll(filepath.Join(ipcDir, "input"), 0755); err != nil {
		return "", "", "", fmt.Errorf("create ipc input dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(ipcDir, "output"), 0755); err != nil {
		return "", "", "", fmt.Errorf("create ipc output dir: %w", err)
	}

	return specDir, stagingDir, ipcDir, nil
}

// startTimeoutMonitor launches a goroutine that kills the container after
// the configured timeout expires. If the container is destroyed before the
// timeout, the monitor is cancelled via context.
// See Architecture Section 12.6: enforce hard timeout per container.
func (m *Manager) startTimeoutMonitor(containerName, containerID, taskID string, containerType ContainerType, timeoutMin int) {
	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	m.timeouts[containerName] = cancel
	m.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			// Container was destroyed normally before timeout.
			return
		case <-time.After(time.Duration(timeoutMin) * time.Minute):
			// Timeout expired. Kill the container.
			m.mu.Lock()
			_, stillActive := m.active[containerName]
			if stillActive {
				delete(m.active, containerName)
				delete(m.timeouts, containerName)
			}
			m.mu.Unlock()

			if !stillActive {
				return
			}

			// Force-remove the timed-out container.
			removeCtx, removeCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer removeCancel()
			_ = m.docker.ContainerRemove(removeCtx, containerID, dockercontainer.RemoveOptions{Force: true})

			// Record timeout in SQLite.
			_ = m.db.UpdateContainerSessionStopped(containerName, time.Now(), "timeout")

			// Emit event for the timeout kill.
			m.emitter.Emit(events.Event{
				Type:      events.EventContainerDestroyed,
				TaskID:    taskID,
				AgentType: string(containerType),
				AgentID:   containerName,
				Details: map[string]interface{}{
					"container_name": containerName,
					"exit_reason":    "timeout",
					"timeout_min":    timeoutMin,
				},
			})
		}
	}()
}

// Shutdown stops all active containers and cancels all timeout monitors.
// Called during engine shutdown.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	names := make([]string, 0, len(m.active))
	for name := range m.active {
		names = append(names, name)
	}
	m.mu.Unlock()

	for _, name := range names {
		_ = m.Destroy(ctx, name)
	}
}
