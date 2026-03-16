package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// mockDockerClient implements DockerClient for testing without a Docker daemon.
type mockDockerClient struct {
	mu             sync.Mutex
	created        []mockCreateCall
	started        []string
	removed        []string
	killed         []string
	listed         []DockerContainerSummary
	createErr      error
	startErr       error
	removeErr      error
	listErr        error
	nextContainerID string
	idCounter      int
}

type mockCreateCall struct {
	Name       string
	Config     *dockercontainer.Config
	HostConfig *dockercontainer.HostConfig
}

func newMockDocker() *mockDockerClient {
	return &mockDockerClient{
		nextContainerID: "mock-container-",
	}
}

func (m *mockDockerClient) ContainerCreate(_ context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, _ interface{}, _ interface{}, containerName string) (dockercontainer.CreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.createErr != nil {
		return dockercontainer.CreateResponse{}, m.createErr
	}

	m.created = append(m.created, mockCreateCall{
		Name:       containerName,
		Config:     config,
		HostConfig: hostConfig,
	})
	m.idCounter++
	return dockercontainer.CreateResponse{
		ID: fmt.Sprintf("%s%d", m.nextContainerID, m.idCounter),
	}, nil
}

func (m *mockDockerClient) ContainerStart(_ context.Context, containerID string, _ dockercontainer.StartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.startErr != nil {
		return m.startErr
	}
	m.started = append(m.started, containerID)
	return nil
}

func (m *mockDockerClient) ContainerStop(_ context.Context, containerID string, _ dockercontainer.StopOptions) error {
	return nil
}

func (m *mockDockerClient) ContainerRemove(_ context.Context, containerID string, _ dockercontainer.RemoveOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.removeErr != nil {
		return m.removeErr
	}
	m.removed = append(m.removed, containerID)
	return nil
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ dockercontainer.ListOptions) ([]DockerContainerSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listed, nil
}

func (m *mockDockerClient) ContainerKill(_ context.Context, containerID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.killed = append(m.killed, containerID)
	return nil
}

// setupTestManager creates a Manager with a mock Docker client, temp directories,
// and a real SQLite database for testing.
func setupTestManager(t *testing.T) (*Manager, *mockDockerClient) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-container-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Create .axiom directory for the DB.
	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatalf("create axiom dir: %v", err)
	}

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	emitter := events.NewEmitter()
	mock := newMockDocker()

	mgr := NewManager(mock, db, emitter, ManagerConfig{
		DefaultImage:   "axiom-meeseeks-multi:latest",
		DefaultCPU:     0.5,
		DefaultMemory:  "2g",
		DefaultTimeout: 30,
		MaxMeeseeks:    10,
		ProjectRoot:    tmpDir,
	})

	return mgr, mock
}

func TestSpawnMeeseeks(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	// Create a task in the DB (required for foreign key constraint).
	err := mgr.db.CreateTask(&state.Task{
		ID:       "task-001",
		Title:    "Test Task",
		Status:   "queued",
		Tier:     "standard",
		TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{
		TaskID:  "task-001",
		ModelID: "anthropic/claude-4-sonnet",
	})
	if err != nil {
		t.Fatalf("spawn meeseeks: %v", err)
	}

	// Verify container was created.
	if len(mock.created) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(mock.created))
	}

	// Verify container name follows pattern: axiom-<task-id>-<timestamp>
	call := mock.created[0]
	if call.Name != result.ContainerName {
		t.Errorf("container name mismatch: create=%s result=%s", call.Name, result.ContainerName)
	}
	if len(call.Name) < len("axiom-task-001-") {
		t.Errorf("container name too short: %s", call.Name)
	}

	// Verify hardening flags.
	hc := call.HostConfig
	if !hc.ReadonlyRootfs {
		t.Error("expected ReadonlyRootfs = true")
	}
	if len(hc.CapDrop) == 0 || hc.CapDrop[0] != "ALL" {
		t.Error("expected CapDrop = [ALL]")
	}
	if len(hc.SecurityOpt) == 0 || hc.SecurityOpt[0] != "no-new-privileges" {
		t.Error("expected SecurityOpt = [no-new-privileges]")
	}
	if hc.PidsLimit == nil || *hc.PidsLimit != 256 {
		t.Error("expected PidsLimit = 256")
	}
	if hc.NetworkMode != "none" {
		t.Errorf("expected NetworkMode = none, got %s", hc.NetworkMode)
	}

	// Verify tmpfs.
	tmpfsOpts, ok := hc.Tmpfs["/tmp"]
	if !ok {
		t.Error("expected tmpfs at /tmp")
	} else if tmpfsOpts != "rw,noexec,size=256m" {
		t.Errorf("expected tmpfs opts 'rw,noexec,size=256m', got '%s'", tmpfsOpts)
	}

	// Verify resource limits.
	expectedNanoCPUs := int64(0.5 * 1e9)
	if hc.NanoCPUs != expectedNanoCPUs {
		t.Errorf("expected NanoCPUs = %d, got %d", expectedNanoCPUs, hc.NanoCPUs)
	}
	expectedMem := ParseMemoryBytes("2g")
	if hc.Memory != expectedMem {
		t.Errorf("expected Memory = %d, got %d", expectedMem, hc.Memory)
	}

	// Verify volume mounts (3 mounts: spec, staging, ipc).
	if len(hc.Mounts) != 3 {
		t.Fatalf("expected 3 mounts, got %d", len(hc.Mounts))
	}
	// Spec mount should be read-only.
	specMount := hc.Mounts[0]
	if specMount.Target != "/workspace/spec" || !specMount.ReadOnly {
		t.Errorf("spec mount incorrect: target=%s readonly=%v", specMount.Target, specMount.ReadOnly)
	}
	// Staging mount should be read-write.
	stagingMount := hc.Mounts[1]
	if stagingMount.Target != "/workspace/staging" || stagingMount.ReadOnly {
		t.Errorf("staging mount incorrect: target=%s readonly=%v", stagingMount.Target, stagingMount.ReadOnly)
	}
	// IPC mount should be read-write.
	ipcMount := hc.Mounts[2]
	if ipcMount.Target != "/workspace/ipc" || ipcMount.ReadOnly {
		t.Errorf("ipc mount incorrect: target=%s readonly=%v", ipcMount.Target, ipcMount.ReadOnly)
	}

	// Verify labels.
	cfg := call.Config
	if cfg.Labels["axiom.task-id"] != "task-001" {
		t.Errorf("expected label axiom.task-id=task-001, got %s", cfg.Labels["axiom.task-id"])
	}
	if cfg.Labels["axiom.container-type"] != "meeseeks" {
		t.Errorf("expected label axiom.container-type=meeseeks, got %s", cfg.Labels["axiom.container-type"])
	}

	// Verify default image was used.
	if cfg.Image != "axiom-meeseeks-multi:latest" {
		t.Errorf("expected default image, got %s", cfg.Image)
	}

	// Verify container was started.
	if len(mock.started) != 1 {
		t.Errorf("expected 1 start call, got %d", len(mock.started))
	}

	// Verify container is tracked.
	if mgr.ActiveCount() != 1 {
		t.Errorf("expected 1 active container, got %d", mgr.ActiveCount())
	}

	// Verify container session was recorded in SQLite.
	session, err := mgr.db.GetContainerSession(result.ContainerName)
	if err != nil {
		t.Fatalf("get container session: %v", err)
	}
	if session.TaskID != "task-001" {
		t.Errorf("expected task-001, got %s", session.TaskID)
	}
	if session.ContainerType != "meeseeks" {
		t.Errorf("expected meeseeks, got %s", session.ContainerType)
	}
}

func TestSpawnDirectoryCreation(t *testing.T) {
	mgr, _ := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-dirs", Title: "Dir Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err = mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-dirs"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Verify host directories were created.
	base := filepath.Join(mgr.config.ProjectRoot, ".axiom", "containers")

	specDir := filepath.Join(base, "specs", "task-dirs")
	if _, err := os.Stat(specDir); os.IsNotExist(err) {
		t.Error("spec directory was not created")
	}

	stagingDir := filepath.Join(base, "staging", "task-dirs")
	if _, err := os.Stat(stagingDir); os.IsNotExist(err) {
		t.Error("staging directory was not created")
	}

	ipcInputDir := filepath.Join(base, "ipc", "task-dirs", "input")
	if _, err := os.Stat(ipcInputDir); os.IsNotExist(err) {
		t.Error("ipc/input directory was not created")
	}

	ipcOutputDir := filepath.Join(base, "ipc", "task-dirs", "output")
	if _, err := os.Stat(ipcOutputDir); os.IsNotExist(err) {
		t.Error("ipc/output directory was not created")
	}
}

func TestDestroy(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-destroy", Title: "Destroy Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-destroy"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Destroy the container.
	err = mgr.Destroy(ctx, result.ContainerName)
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}

	// Verify container was removed.
	if len(mock.removed) < 1 {
		t.Error("expected at least 1 remove call")
	}

	// Verify container is no longer tracked.
	if mgr.ActiveCount() != 0 {
		t.Errorf("expected 0 active containers, got %d", mgr.ActiveCount())
	}

	// Verify session was updated in SQLite.
	session, err := mgr.db.GetContainerSession(result.ContainerName)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if session.ExitReason != "completed" {
		t.Errorf("expected exit_reason=completed, got %s", session.ExitReason)
	}
	if session.StoppedAt == nil {
		t.Error("expected stopped_at to be set")
	}
}

func TestConcurrencyLimit(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.config.MaxMeeseeks = 2
	ctx := context.Background()

	// Create tasks.
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("task-conc-%d", i)
		err := mgr.db.CreateTask(&state.Task{
			ID: id, Title: "Concurrency Test", Status: "queued", Tier: "standard", TaskType: "implementation",
		})
		if err != nil {
			t.Fatalf("create task %s: %v", id, err)
		}
	}

	// Spawn 2 containers (should succeed).
	_, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-conc-1"})
	if err != nil {
		t.Fatalf("spawn 1: %v", err)
	}
	_, err = mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-conc-2"})
	if err != nil {
		t.Fatalf("spawn 2: %v", err)
	}

	// Spawn 3rd should fail (at limit).
	_, err = mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-conc-3"})
	if err == nil {
		t.Error("expected concurrency limit error, got nil")
	}

	// Verify active count.
	if mgr.ActiveCount() != 2 {
		t.Errorf("expected 2 active containers, got %d", mgr.ActiveCount())
	}
}

func TestValidatorsNotCountedAgainstLimit(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.config.MaxMeeseeks = 1
	ctx := context.Background()

	// Create tasks.
	for _, id := range []string{"task-val-1", "task-val-2"} {
		err := mgr.db.CreateTask(&state.Task{
			ID: id, Title: "Validator Test", Status: "queued", Tier: "standard", TaskType: "implementation",
		})
		if err != nil {
			t.Fatalf("create task %s: %v", id, err)
		}
	}

	// Spawn 1 Meeseeks (hits the limit).
	_, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-val-1"})
	if err != nil {
		t.Fatalf("spawn meeseeks: %v", err)
	}

	// Spawn a validator (should succeed despite limit, validators are exempt).
	_, err = mgr.SpawnValidator(ctx, SpawnRequest{TaskID: "task-val-2"})
	if err != nil {
		t.Fatalf("spawn validator: %v", err)
	}

	// ActiveCount should be 1 (only counts non-validators).
	if mgr.ActiveCount() != 1 {
		t.Errorf("expected 1 active agent container, got %d", mgr.ActiveCount())
	}

	// Total tracked should be 2.
	if len(mgr.ListActive()) != 2 {
		t.Errorf("expected 2 total tracked containers, got %d", len(mgr.ListActive()))
	}
}

func TestCleanupOrphans(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	// Simulate orphaned containers from a previous session.
	mock.listed = []DockerContainerSummary{
		{ID: "orphan-1", Names: []string{"/axiom-old-task-1"}, State: "running"},
		{ID: "orphan-2", Names: []string{"/axiom-old-task-2"}, State: "exited"},
	}

	cleaned, err := mgr.CleanupOrphans(ctx)
	if err != nil {
		t.Fatalf("cleanup orphans: %v", err)
	}

	if cleaned != 2 {
		t.Errorf("expected 2 orphans cleaned, got %d", cleaned)
	}

	// Verify remove was called for both.
	if len(mock.removed) != 2 {
		t.Errorf("expected 2 remove calls, got %d", len(mock.removed))
	}
}

func TestSpawnCreateFailure(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-fail", Title: "Fail Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Simulate Docker create failure.
	mock.createErr = fmt.Errorf("image not found")

	_, err = mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-fail"})
	if err == nil {
		t.Error("expected error from docker create failure")
	}

	// Verify nothing is tracked.
	if mgr.ActiveCount() != 0 {
		t.Errorf("expected 0 active containers after failure, got %d", mgr.ActiveCount())
	}
}

func TestSpawnStartFailureCleanup(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-start-fail", Title: "Start Fail", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Simulate Docker start failure (create succeeds, start fails).
	mock.startErr = fmt.Errorf("no space on device")

	_, err = mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-start-fail"})
	if err == nil {
		t.Error("expected error from docker start failure")
	}

	// Verify the created-but-unstarted container was cleaned up.
	if len(mock.removed) != 1 {
		t.Errorf("expected 1 remove call for cleanup, got %d", len(mock.removed))
	}

	// Verify nothing is tracked.
	if mgr.ActiveCount() != 0 {
		t.Errorf("expected 0 active containers after start failure, got %d", mgr.ActiveCount())
	}
}

func TestTimeoutEnforcement(t *testing.T) {
	mgr, mock := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-timeout", Title: "Timeout Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Use a very short timeout for testing (we can't use 0 so use the actual
	// timeout mechanism but verify the goroutine runs).
	// NOTE: We test the timeout monitor indirectly by verifying it was started
	// and can be cancelled. A real timeout test would need a 1-minute+ wait.
	result, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{
		TaskID:     "task-timeout",
		TimeoutMin: 30, // Default
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Verify timeout cancel function was registered.
	mgr.mu.Lock()
	_, hasTimeout := mgr.timeouts[result.ContainerName]
	mgr.mu.Unlock()
	if !hasTimeout {
		t.Error("expected timeout monitor to be registered")
	}

	// Destroy should cancel the timeout.
	err = mgr.Destroy(ctx, result.ContainerName)
	if err != nil {
		t.Fatalf("destroy: %v", err)
	}

	mgr.mu.Lock()
	_, hasTimeout = mgr.timeouts[result.ContainerName]
	mgr.mu.Unlock()
	if hasTimeout {
		t.Error("expected timeout to be cancelled after destroy")
	}

	_ = mock // suppress unused warning
}

func TestContainerNamingConvention(t *testing.T) {
	mgr, _ := setupTestManager(t)
	ctx := context.Background()

	err := mgr.db.CreateTask(&state.Task{
		ID: "my-task-42", Title: "Naming Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "my-task-42"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Name should start with "axiom-" prefix.
	if len(result.ContainerName) < 6 || result.ContainerName[:6] != "axiom-" {
		t.Errorf("container name should start with 'axiom-', got: %s", result.ContainerName)
	}

	// Name should contain the task ID.
	expected := "axiom-my-task-42-"
	if len(result.ContainerName) < len(expected) || result.ContainerName[:len(expected)] != expected {
		t.Errorf("container name should match 'axiom-<task-id>-<timestamp>', got: %s", result.ContainerName)
	}
}

func TestEventEmission(t *testing.T) {
	mgr, _ := setupTestManager(t)
	ctx := context.Background()

	var spawnEvents []events.Event
	var destroyEvents []events.Event
	mgr.emitter.Subscribe(events.EventContainerSpawned, func(e events.Event) {
		spawnEvents = append(spawnEvents, e)
	})
	mgr.emitter.Subscribe(events.EventContainerDestroyed, func(e events.Event) {
		destroyEvents = append(destroyEvents, e)
	})

	err := mgr.db.CreateTask(&state.Task{
		ID: "task-events", Title: "Event Test", Status: "queued", Tier: "standard", TaskType: "implementation",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	result, err := mgr.SpawnMeeseeks(ctx, SpawnRequest{TaskID: "task-events"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// Wait for async event delivery.
	time.Sleep(100 * time.Millisecond)

	if len(spawnEvents) != 1 {
		t.Errorf("expected 1 spawn event, got %d", len(spawnEvents))
	}

	_ = mgr.Destroy(ctx, result.ContainerName)
	time.Sleep(100 * time.Millisecond)

	if len(destroyEvents) != 1 {
		t.Errorf("expected 1 destroy event, got %d", len(destroyEvents))
	}
}

func TestParseMemoryBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"2g", 2 * 1024 * 1024 * 1024},
		{"4g", 4 * 1024 * 1024 * 1024},
		{"512m", 512 * 1024 * 1024},
		{"256m", 256 * 1024 * 1024},
		{"1024k", 1024 * 1024},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		result := ParseMemoryBytes(tt.input)
		if result != tt.expected {
			t.Errorf("ParseMemoryBytes(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}
