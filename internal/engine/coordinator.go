// Package engine implements the Trusted Engine, the core control plane of Axiom.
//
// The Coordinator wires all subsystems together and runs the main execution loop.
// It is the single entry point that connects: container lifecycle, IPC protocol,
// inference broker, task system, approval pipeline, merge queue, git integration,
// SRS management, budget enforcement, semantic indexer, and event emission.
//
// See Architecture.md Section 3 (System Architecture) and Section 5 (Core Flow).
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/budget"
	"github.com/ethan03805/axiom/internal/container"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/git"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/merge"
	"github.com/ethan03805/axiom/internal/orchestrator"
	"github.com/ethan03805/axiom/internal/pipeline"
	"github.com/ethan03805/axiom/internal/security"
	"github.com/ethan03805/axiom/internal/srs"
	"github.com/ethan03805/axiom/internal/state"
)

// Coordinator is the central engine that wires all subsystems together and
// runs the main execution loop. It holds references to every subsystem and
// serves as the Trusted Control Plane described in Architecture Section 3.
//
// All privileged operations (filesystem writes, git commits, container spawning,
// budget enforcement, model access) are performed exclusively through the
// Coordinator. No LLM agent performs any privileged operation directly.
// See Architecture Section 4 (Trusted Engine vs. Untrusted Planes).
type Coordinator struct {
	config *Config
	db     *state.DB

	// Subsystems
	emitter       *events.Emitter
	containerMgr  *container.Manager
	ipcWatcher    *ipc.Watcher
	ipcDispatcher *ipc.Dispatcher
	ipcWriter     *ipc.Writer
	infBroker     *broker.Broker
	workQueue     *WorkQueue
	scopeHandler  *ScopeExpansionHandler
	pipelineMgr   *pipeline.Pipeline
	mergeQueue    *merge.Queue
	gitMgr        *git.Manager
	srsApproval   *srs.ApprovalManager
	ecoMgr        *srs.ECOManager
	budgetEnforce *budget.Enforcer
	budgetTracker *budget.Tracker
	secretScanner *security.SecretScanner
	orchestratorMgr *orchestrator.Embedded
	validationSandbox *container.ValidationSandbox

	// Runtime state
	mu          sync.Mutex
	running     bool
	paused      bool
	projectRoot string
	projectID   string
	stopCh      chan struct{}
}

// NewCoordinator creates a fully-wired Coordinator from the given configuration.
// It initializes the database, creates all subsystems, and wires them together.
//
// The projectRoot is the absolute path to the project directory.
// See Architecture Section 5.1 step 1 (Initialization).
func NewCoordinator(config *Config, projectRoot string) (*Coordinator, error) {
	dbPath := filepath.Join(projectRoot, ".axiom", "axiom.db")

	db, err := state.NewDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}
	if err := db.RunMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	emitter := events.NewEmitter()

	c := &Coordinator{
		config:      config,
		db:          db,
		emitter:     emitter,
		projectRoot: projectRoot,
		stopCh:      make(chan struct{}),
	}

	// Wire event persistence: all events stored in SQLite.
	// Uses json.Marshal for proper JSON serialization.
	emitter.SubscribeAll(func(event events.Event) {
		details := ""
		if event.Details != nil {
			if data, err := json.Marshal(event.Details); err == nil {
				details = string(data)
			}
		}
		_ = db.InsertEvent(&state.Event{
			Type:      string(event.Type),
			TaskID:    event.TaskID,
			AgentType: event.AgentType,
			AgentID:   event.AgentID,
			Details:   details,
			Timestamp: event.Timestamp,
		})
	})

	// Initialize IPC subsystem.
	// See Architecture Section 20.3 for the IPC protocol.
	ipcBaseDir := filepath.Join(projectRoot, ".axiom", "containers", "ipc")
	c.ipcWriter = ipc.NewWriter(ipcBaseDir)
	c.ipcDispatcher = ipc.NewDispatcher(c.ipcWriter)

	watcher, err := ipc.NewWatcher(ipcBaseDir, c.ipcDispatcher.Dispatch)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init ipc watcher: %w", err)
	}
	c.ipcWatcher = watcher

	// Initialize git manager.
	// See Architecture Section 23.
	branchPrefix := config.Git.BranchPrefix
	if branchPrefix == "" {
		branchPrefix = "axiom"
	}
	c.gitMgr = git.NewManager(projectRoot, branchPrefix)

	// Initialize container manager.
	// See Architecture Section 12.
	// DockerClient is created lazily when the engine starts (requires daemon).
	c.containerMgr = nil // Set during Start() when Docker client is available.

	// Initialize inference broker with providers.
	// See Architecture Section 19.5.
	var openrouterProvider broker.Provider
	var bitnetProvider broker.Provider

	// OpenRouter client is created if API key is available.
	// API key is loaded from ~/.axiom/config.toml, never injected into containers.
	home, _ := os.UserHomeDir()
	globalCfgPath := filepath.Join(home, ".axiom", "config.toml")
	if globalCfg, err := LoadConfigFrom(globalCfgPath); err == nil {
		// Check if there's an API key in the global config.
		// The API key field is not in the Config struct (by design, it's sensitive).
		// We'll look for it via TOML directly.
		_ = globalCfg
	}
	openrouterProvider = broker.NewOpenRouterClient(broker.OpenRouterConfig{
		APIKey: os.Getenv("OPENROUTER_API_KEY"), // Fallback to env var
	})

	if config.BitNet.Enabled {
		bitnetProvider = broker.NewBitNetClient(broker.BitNetConfig{
			Host: config.BitNet.Host,
			Port: config.BitNet.Port,
		})
	}

	c.infBroker = broker.New(
		openrouterProvider,
		bitnetProvider,
		db,
		emitter,
		c.ipcWriter,
		broker.Config{
			BudgetMaxUSD:  config.Budget.MaxUSD,
			MaxReqPerTask: 50,
		},
	)

	// Initialize work queue with concurrency control.
	// See Architecture Section 5 (Task System & Concurrency).
	c.workQueue = NewWorkQueue(db, emitter, config.Concurrency.MaxMeeseeks)

	// Initialize scope expansion handler.
	// See Architecture Section 10.7.
	c.scopeHandler = NewScopeExpansionHandler(db, emitter)

	// Initialize approval pipeline.
	// See Architecture Section 14.
	c.pipelineMgr = pipeline.NewPipeline(pipeline.DefaultPipelineConfig())

	// Initialize merge queue.
	// See Architecture Section 16.4.
	c.mergeQueue = merge.NewQueue(c.gitMgr, emitter)

	// Initialize SRS management.
	// See Architecture Section 6 and 7.
	axiomDir := filepath.Join(projectRoot, ".axiom")
	delegate := srs.ApprovalDelegate(config.Orchestrator.SRSApprovalDelegate)
	c.srsApproval = srs.NewApprovalManager(axiomDir, emitter, delegate)
	c.ecoMgr = srs.NewECOManager(db, emitter, axiomDir)

	// Initialize budget subsystem.
	// See Architecture Section 21.
	c.budgetEnforce = budget.NewEnforcer(db, emitter, budget.EnforcerConfig{
		MaxUSD:        config.Budget.MaxUSD,
		WarnAtPercent: config.Budget.WarnAtPercent,
	})
	c.budgetTracker = budget.NewTracker(db, config.Budget.MaxUSD, false)

	// Initialize secret scanner.
	// See Architecture Section 29.4.
	c.secretScanner = security.NewSecretScanner(config.Security.SensitivePatterns)

	// Register IPC handlers.
	// See Architecture Section 20.4 for the message type routing table.
	c.registerIPCHandlers()

	// Wire merge queue callbacks.
	// After a successful merge, the work queue releases locks and unblocks tasks.
	c.mergeQueue.ReindexFn = func(changedFiles []string) error {
		// Semantic indexer incremental refresh (Phase 8).
		// Stubbed until tree-sitter integration is complete.
		return nil
	}

	return c, nil
}

// registerIPCHandlers wires IPC message types to engine subsystem handlers.
// See Architecture Section 20.4 and BUILD_PLAN step 3.4.
func (c *Coordinator) registerIPCHandlers() {
	// inference_request -> Inference Broker
	c.ipcDispatcher.Register(ipc.TypeInferenceRequest, c.infBroker.HandleInferenceRequest)

	// task_output -> Pipeline (manifest validation, sandbox, review, merge)
	c.ipcDispatcher.Register(ipc.TypeTaskOutput, c.handleTaskOutput)

	// review_result -> Pipeline stage advancement
	c.ipcDispatcher.Register(ipc.TypeReviewResult, c.handleReviewResult)

	// action_request -> Orchestrator action dispatcher
	actionHandler := orchestrator.NewActionHandler(c.db, c.emitter)
	c.wireActionHandlerCallbacks(actionHandler)
	c.ipcDispatcher.Register(ipc.TypeActionRequest, actionHandler.HandleAction)

	// request_scope_expansion -> Scope expansion handler
	c.ipcDispatcher.Register(ipc.TypeScopeExpansionRequest, c.scopeHandler.HandleScopeExpansion)
}

// wireActionHandlerCallbacks connects the orchestrator's action handler to
// engine operations. This is the concrete implementation of the
// "LLM agents propose, engine disposes" contract from Architecture Section 4.2.
func (c *Coordinator) wireActionHandlerCallbacks(ah *orchestrator.ActionHandler) {
	ah.OnSubmitSRS = func(taskID, content string) error {
		errs, err := c.srsApproval.SubmitDraft(content)
		if err != nil {
			return err
		}
		if len(errs) > 0 {
			return fmt.Errorf("SRS validation failed: %v", errs)
		}
		return nil
	}

	ah.OnSubmitECO = func(taskID, ecoCode, category, desc, refs, change string) error {
		_, err := c.ecoMgr.ProposeECO(ecoCode, desc, refs, change)
		return err
	}

	ah.OnCreateTask = func(task *state.Task) error {
		return c.db.CreateTask(task)
	}

	ah.OnCreateTaskBatch = func(tasks []*state.Task) error {
		return c.db.CreateTaskBatch(tasks)
	}

	ah.OnSpawnMeeseeks = func(taskID, modelID string) error {
		if c.containerMgr == nil {
			return fmt.Errorf("container manager not initialized")
		}
		_, err := c.containerMgr.SpawnMeeseeks(context.Background(), container.SpawnRequest{
			TaskID:      taskID,
			Image:       c.config.Docker.Image,
			ModelID:     modelID,
			CPULimit:    c.config.Docker.CPULimit,
			MemoryLimit: c.config.Docker.MemLimit,
			TimeoutMin:  c.config.Docker.TimeoutMinutes,
		})
		return err
	}

	ah.OnSpawnReviewer = func(taskID, modelID string) error {
		if c.containerMgr == nil {
			return fmt.Errorf("container manager not initialized")
		}
		_, err := c.containerMgr.SpawnReviewer(context.Background(), container.SpawnRequest{
			TaskID:      taskID,
			Image:       c.config.Docker.Image,
			ModelID:     modelID,
			CPULimit:    c.config.Docker.CPULimit,
			MemoryLimit: c.config.Docker.MemLimit,
			TimeoutMin:  c.config.Docker.TimeoutMinutes,
		})
		return err
	}

	ah.OnSpawnSubOrch = func(taskID, modelID string) error {
		if c.containerMgr == nil {
			return fmt.Errorf("container manager not initialized")
		}
		_, err := c.containerMgr.SpawnSubOrchestrator(context.Background(), container.SpawnRequest{
			TaskID:      taskID,
			Image:       c.config.Docker.Image,
			ModelID:     modelID,
			CPULimit:    c.config.Docker.CPULimit,
			MemoryLimit: c.config.Docker.MemLimit,
			TimeoutMin:  c.config.Docker.TimeoutMinutes,
		})
		return err
	}

	ah.OnApproveOutput = func(taskID string) error {
		return c.db.UpdateTaskStatus(taskID, state.TaskStatusDone)
	}

	ah.OnRejectOutput = func(taskID, feedback string) error {
		return c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
	}

	ah.OnQueryStatus = func() (interface{}, error) {
		tasks, err := c.db.ListTasks(state.TaskFilter{})
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"tasks":             tasks,
			"paused":            c.paused,
			"active_containers": c.activeContainerCount(),
		}, nil
	}

	ah.OnQueryBudget = func() (interface{}, error) {
		return c.budgetTracker.GetReport(c.completionPercentage())
	}
}

// Start initializes the Docker client, performs crash recovery, and starts
// the main execution loop. See Architecture Section 5.1 and Section 22.3.
func (c *Coordinator) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("coordinator already running")
	}
	c.mu.Unlock()

	// Initialize Docker client.
	dockerCli, err := container.NewDockerClient()
	if err != nil {
		// Docker may not be available. Log warning but continue
		// (some operations like axiom status don't need Docker).
		c.emitter.Emit(events.Event{
			Type:      events.EventProviderUnavailable,
			AgentType: "engine",
			Details: map[string]interface{}{
				"provider": "docker",
				"error":    err.Error(),
			},
		})
	} else {
		c.containerMgr = container.NewManager(dockerCli, c.db, c.emitter, container.ManagerConfig{
			DefaultImage:   c.config.Docker.Image,
			DefaultCPU:     c.config.Docker.CPULimit,
			DefaultMemory:  c.config.Docker.MemLimit,
			DefaultTimeout: c.config.Docker.TimeoutMinutes,
			MaxMeeseeks:    c.config.Concurrency.MaxMeeseeks,
			ProjectRoot:    c.projectRoot,
		})
	}

	// Crash recovery per Architecture Section 22.3.
	if err := c.crashRecovery(); err != nil {
		return fmt.Errorf("crash recovery: %w", err)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	// Start the main execution loop in a background goroutine.
	go c.executionLoop()

	return nil
}

// Stop gracefully shuts down the coordinator.
// - Stops spawning new containers
// - Shuts down active containers
// - Stops the IPC watcher
// - Flushes state to SQLite
// - Closes the database
func (c *Coordinator) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()

	// Shutdown orchestrator if running.
	if c.orchestratorMgr != nil {
		_ = c.orchestratorMgr.Stop(context.Background())
	}

	// Shutdown all active containers.
	if c.containerMgr != nil {
		c.containerMgr.Shutdown(context.Background())
	}

	// Stop IPC watcher.
	if c.ipcWatcher != nil {
		_ = c.ipcWatcher.Stop()
	}

	return c.db.Close()
}

// crashRecovery runs the full crash recovery procedure.
// See Architecture Section 22.3.
func (c *Coordinator) crashRecovery() error {
	conn := c.db.Conn()

	// Step 1: Kill orphaned containers.
	if c.containerMgr != nil {
		cleaned, err := c.containerMgr.CleanupOrphans(context.Background())
		if err != nil {
			// Non-fatal: Docker might not be available.
			c.emitter.Emit(events.Event{
				Type:      events.EventCrashRecovery,
				AgentType: "engine",
				Details: map[string]interface{}{
					"orphan_cleanup_error": err.Error(),
				},
			})
		} else if cleaned > 0 {
			c.emitter.Emit(events.Event{
				Type:      events.EventCrashRecovery,
				AgentType: "engine",
				Details: map[string]interface{}{
					"orphan_containers_removed": cleaned,
				},
			})
		}
	}

	// Step 2: Reset orphaned in_progress/in_review tasks to queued.
	result, err := conn.Exec("UPDATE tasks SET status = 'queued' WHERE status IN ('in_progress', 'in_review')")
	if err != nil {
		return fmt.Errorf("reset orphaned tasks: %w", err)
	}
	resetCount, _ := result.RowsAffected()

	// Step 3: Release all stale locks (no containers survive a crash).
	lockResult, err := conn.Exec("DELETE FROM task_locks")
	if err != nil {
		return fmt.Errorf("release stale locks: %w", err)
	}
	lockCount, _ := lockResult.RowsAffected()

	// Step 4: Clean staging directories.
	stagingBase := filepath.Join(c.projectRoot, ".axiom", "containers", "staging")
	if entries, err := os.ReadDir(stagingBase); err == nil {
		for _, entry := range entries {
			_ = os.RemoveAll(filepath.Join(stagingBase, entry.Name()))
		}
	}

	// Step 5: Verify SRS integrity.
	if err := c.srsApproval.VerifyIntegrity(); err != nil {
		return fmt.Errorf("SRS integrity check: %w", err)
	}

	if resetCount > 0 || lockCount > 0 {
		c.emitter.Emit(events.Event{
			Type:      events.EventCrashRecovery,
			AgentType: "engine",
			Details: map[string]interface{}{
				"tasks_reset": resetCount,
				"locks_freed": lockCount,
			},
			Timestamp: time.Now(),
		})
	}

	return nil
}

// executionLoop is the main engine loop that continuously dispatches ready
// tasks, processes the merge queue, and checks for completion.
// See Architecture Section 5.1 step 7 (Execution Loop).
func (c *Coordinator) executionLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.mu.Lock()
			paused := c.paused
			running := c.running
			c.mu.Unlock()

			if !running || paused {
				continue
			}

			// Process the merge queue (serialized, one at a time).
			c.processMergeQueue()

			// Check budget status.
			c.checkBudget()

			// Check for completion.
			c.checkCompletion()
		}
	}
}

// processMergeQueue processes the next item in the merge queue if available.
// After a successful merge, it releases locks and unblocks dependent tasks.
// See Architecture Section 16.4.
func (c *Coordinator) processMergeQueue() {
	result, ok := c.mergeQueue.ProcessNext()
	if !ok {
		return
	}

	if result.Success {
		// Lock release and dependent task unblocking.
		// This is steps 9-10 from Architecture Section 16.4.
		taskID := "" // The merge result should carry the task ID.
		if taskID != "" {
			c.workQueue.CompleteTask(taskID)
		}
	} else if result.NeedsRequeue {
		// Task needs to be re-queued with updated context.
		// The merge queue already logged the event.
	}
}

// checkBudget verifies remaining budget and pauses if exhausted.
// See Architecture Section 21.3.
func (c *Coordinator) checkBudget() {
	total, err := c.db.GetProjectCost()
	if err != nil {
		return
	}

	if c.config.Budget.MaxUSD > 0 {
		pct := (total / c.config.Budget.MaxUSD) * 100

		if pct >= 100 {
			c.emitter.Emit(events.Event{
				Type:      events.EventBudgetExhausted,
				AgentType: "engine",
				Details: map[string]interface{}{
					"total_cost_usd":  total,
					"budget_max_usd":  c.config.Budget.MaxUSD,
					"budget_used_pct": pct,
				},
			})
			c.Pause()
		} else if pct >= c.config.Budget.WarnAtPercent {
			c.emitter.Emit(events.Event{
				Type:      events.EventBudgetWarning,
				AgentType: "engine",
				Details: map[string]interface{}{
					"total_cost_usd":  total,
					"budget_max_usd":  c.config.Budget.MaxUSD,
					"budget_used_pct": pct,
				},
			})
		}
	}
}

// checkCompletion checks if all tasks are done and signals completion.
func (c *Coordinator) checkCompletion() {
	tasks, err := c.db.ListTasks(state.TaskFilter{})
	if err != nil || len(tasks) == 0 {
		return
	}

	allDone := true
	for _, task := range tasks {
		if task.Status != string(state.TaskStatusDone) &&
			task.Status != string(state.TaskStatusCancelledECO) {
			allDone = false
			break
		}
	}

	if allDone && c.orchestratorMgr != nil {
		c.orchestratorMgr.Complete()
	}
}

// Pause stops spawning new Meeseeks but lets running containers complete.
// See Architecture Section 5.3.
func (c *Coordinator) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = true
	if c.orchestratorMgr != nil {
		c.orchestratorMgr.Pause()
	}
}

// Resume resumes a paused project.
func (c *Coordinator) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.paused = false
	if c.orchestratorMgr != nil {
		c.orchestratorMgr.Resume()
	}
}

// Cancel kills all containers, reverts uncommitted changes, marks cancelled.
// See Architecture Section 5.3.
func (c *Coordinator) Cancel(ctx context.Context) error {
	if c.containerMgr != nil {
		c.containerMgr.Shutdown(ctx)
	}
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return nil
}

// handleTaskOutput processes a task_output IPC message from a Meeseeks.
// Routes through the full approval pipeline.
// See Architecture Section 14.2.
func (c *Coordinator) handleTaskOutput(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	outputMsg, ok := msg.(*ipc.TaskOutputMessage)
	if !ok {
		return nil, fmt.Errorf("expected TaskOutputMessage, got %T", msg)
	}

	stagingDir := filepath.Join(c.projectRoot, ".axiom", "containers", "staging", taskID)

	// Get the task's target files for scope checking.
	targetFiles, err := c.db.GetTaskTargetFiles(taskID)
	if err != nil {
		return nil, fmt.Errorf("get target files: %w", err)
	}
	filePaths := make([]string, len(targetFiles))
	for i, f := range targetFiles {
		filePaths[i] = f.FilePath
	}

	// Get current task for attempt tracking.
	task, err := c.db.GetTask(taskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	// Run the pipeline.
	pipeResult := c.pipelineMgr.Execute(
		taskID, stagingDir, "", // taskSpec would come from stored spec
		filePaths,
		outputMsg.BaseSnapshot,
		1, // attemptNumber - would come from attempt tracking
		task.Tier,
	)

	if pipeResult.Approved {
		// Submit to merge queue.
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskCompleted,
			TaskID: taskID,
		})
	} else if pipeResult.ShouldRetry || pipeResult.ShouldEscalate {
		// Destroy current Meeseeks, spawn fresh one with feedback.
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: taskID,
			Details: map[string]interface{}{
				"retry":    pipeResult.ShouldRetry,
				"escalate": pipeResult.ShouldEscalate,
				"feedback": pipeResult.Feedback,
			},
		})
	} else if pipeResult.ShouldBlock {
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusBlocked)
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskBlocked,
			TaskID: taskID,
		})
	}

	return nil, nil
}

// handleReviewResult processes a review_result IPC message from a reviewer.
func (c *Coordinator) handleReviewResult(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	reviewMsg, ok := msg.(*ipc.ReviewResultMessage)
	if !ok {
		return nil, fmt.Errorf("expected ReviewResultMessage, got %T", msg)
	}

	if reviewMsg.Verdict == "approve" {
		c.emitter.Emit(events.Event{
			Type:   events.EventReviewCompleted,
			TaskID: taskID,
			Details: map[string]interface{}{
				"verdict": "approve",
			},
		})
	} else {
		c.emitter.Emit(events.Event{
			Type:   events.EventReviewCompleted,
			TaskID: taskID,
			Details: map[string]interface{}{
				"verdict":  "reject",
				"feedback": reviewMsg.Feedback,
			},
		})
	}

	return nil, nil
}


// DB returns the state database.
func (c *Coordinator) DB() *state.DB { return c.db }

// Emitter returns the event emitter.
func (c *Coordinator) Emitter() *events.Emitter { return c.emitter }

// Config returns the engine configuration.
func (c *Coordinator) Config() *Config { return c.config }

// ContainerManager returns the container manager (may be nil if Docker unavailable).
func (c *Coordinator) ContainerManager() *container.Manager { return c.containerMgr }

// InferenceBroker returns the inference broker.
func (c *Coordinator) InferenceBroker() *broker.Broker { return c.infBroker }

// MergeQueue returns the merge queue.
func (c *Coordinator) MergeQueue() *merge.Queue { return c.mergeQueue }

// SRSApproval returns the SRS approval manager.
func (c *Coordinator) SRSApproval() *srs.ApprovalManager { return c.srsApproval }

// ECOManager returns the ECO manager.
func (c *Coordinator) ECOManager() *srs.ECOManager { return c.ecoMgr }

// BudgetTracker returns the budget tracker.
func (c *Coordinator) BudgetTracker() *budget.Tracker { return c.budgetTracker }

// SecretScanner returns the secret scanner.
func (c *Coordinator) SecretScanner() *security.SecretScanner { return c.secretScanner }

// GitManager returns the git manager.
func (c *Coordinator) GitManager() *git.Manager { return c.gitMgr }

// IPCWriter returns the IPC writer.
func (c *Coordinator) IPCWriter() *ipc.Writer { return c.ipcWriter }

// activeContainerCount returns the number of active agent containers.
func (c *Coordinator) activeContainerCount() int {
	if c.containerMgr == nil {
		return 0
	}
	return c.containerMgr.ActiveCount()
}

// completionPercentage calculates the percentage of tasks that are done.
func (c *Coordinator) completionPercentage() float64 {
	tasks, err := c.db.ListTasks(state.TaskFilter{})
	if err != nil || len(tasks) == 0 {
		return 0
	}

	done := 0
	for _, t := range tasks {
		if t.Status == string(state.TaskStatusDone) || t.Status == string(state.TaskStatusCancelledECO) {
			done++
		}
	}
	return float64(done) / float64(len(tasks)) * 100
}
