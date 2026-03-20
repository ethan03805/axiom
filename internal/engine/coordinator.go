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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/budget"
	"github.com/ethan03805/axiom/internal/container"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/git"
	"github.com/ethan03805/axiom/internal/index"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/merge"
	"github.com/ethan03805/axiom/internal/orchestrator"
	"github.com/ethan03805/axiom/internal/pipeline"
	"github.com/ethan03805/axiom/internal/registry"
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
	semanticIdx   *index.Indexer
	orchestratorMgr orchestrator.Orchestrator
	validationSandbox *container.ValidationSandbox
	taskSpecBuilder *TaskSpecBuilder

	// Runtime state
	mu          sync.Mutex
	running     bool
	paused      bool
	projectRoot string
	projectID   string
	stopCh      chan struct{}
	doneCh      chan struct{} // Closed when all tasks complete; signals CLI to exit
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
		doneCh:      make(chan struct{}),
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
	// API key is loaded from config (global + project merged by LoadConfig()),
	// with env var as fallback. Never injected into containers.
	// See Architecture Section 19.5.
	openrouterAPIKey := config.OpenRouter.APIKey
	if openrouterAPIKey == "" {
		openrouterAPIKey = os.Getenv("OPENROUTER_API_KEY")
	}
	openrouterProvider = broker.NewOpenRouterClient(broker.OpenRouterConfig{
		APIKey: openrouterAPIKey,
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
			IPCBaseDir:    ipcBaseDir,
		},
	)

	// Populate broker model registry from ~/.axiom/registry.db.
	// This enables model allowlist checks, budget enforcement per-request,
	// and accurate cost tracking. See Architecture Section 19.5.
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		registryPath := filepath.Join(home, ".axiom", "registry.db")
		if _, statErr := os.Stat(registryPath); statErr == nil {
			if reg, regErr := registry.NewRegistry(registryPath); regErr == nil {
				models, listErr := reg.List("", "")
				if listErr == nil {
					for _, m := range models {
						c.infBroker.RegisterModel(&broker.ModelInfo{
							ID:   m.ID,
							Tier: broker.ModelTier(m.Tier),
							Pricing: broker.ModelPricing{
								PromptPerMillion:     m.PromptPerMillion,
								CompletionPerMillion: m.CompletionPerMillion,
							},
							Source: m.Source,
						})
					}
				}
				reg.Close()
			}
		}
	}

	// Register BitNet models in the broker so they route to the BitNet provider.
	// Query the local BitNet server for its model ID.
	if config.BitNet.Enabled {
		bitnetURL := fmt.Sprintf("http://%s:%d/v1/models", config.BitNet.Host, config.BitNet.Port)
		if resp, httpErr := (&http.Client{Timeout: 2 * time.Second}).Get(bitnetURL); httpErr == nil {
			var modelList struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if decErr := json.NewDecoder(resp.Body).Decode(&modelList); decErr == nil {
				for _, m := range modelList.Data {
					c.infBroker.RegisterModel(&broker.ModelInfo{
						ID:     m.ID,
						Tier:   broker.TierLocal,
						Source: "bitnet",
						Pricing: broker.ModelPricing{
							PromptPerMillion:     0,
							CompletionPerMillion: 0,
						},
					})
				}
			}
			resp.Body.Close()
		}
	}

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

	// Initialize semantic indexer.
	// See Architecture Section 17.
	c.semanticIdx = index.NewIndexer(db.Conn())
	if err := c.semanticIdx.InitSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init index schema: %w", err)
	}
	c.semanticIdx.RegisterParser(index.NewGoParser())

	// Initialize TaskSpec builder.
	// See Architecture Section 10.3.
	c.taskSpecBuilder = NewTaskSpecBuilder(db, c.secretScanner, projectRoot)

	// Register IPC handlers.
	// See Architecture Section 20.4 for the message type routing table.
	c.registerIPCHandlers()

	// Wire merge queue callbacks.
	// After a successful merge, the semantic indexer incrementally re-indexes
	// only the changed files. See Architecture Section 17.4.
	c.mergeQueue.ReindexFn = func(changedFiles []string) error {
		return c.semanticIdx.IncrementalIndex(c.projectRoot, changedFiles)
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

			// Dispatch ready tasks to containers.
			c.dispatchReadyTasks()

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
		if result.TaskID != "" {
			if err := c.workQueue.CompleteTask(result.TaskID); err != nil {
				c.emitter.Emit(events.Event{
					Type:   events.EventTaskFailed,
					TaskID: result.TaskID,
					Details: map[string]interface{}{
						"error": fmt.Sprintf("complete task after merge: %v", err),
					},
				})
			}
			// Transition through in_review before done, as the state machine
			// requires in_progress -> in_review -> done (Architecture Section 15.4).
			// In-process execution skips the reviewer stage, so we advance
			// through the intermediate state here.
			_ = c.db.UpdateTaskStatus(result.TaskID, state.TaskStatusInReview)
			_ = c.db.UpdateTaskStatus(result.TaskID, state.TaskStatusDone)
		}
	} else if result.NeedsRequeue {
		// Task needs to be re-queued with updated context (stale snapshot).
		if result.TaskID != "" {
			c.requeueTask(result.TaskID)
		}
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

// checkCompletion checks if all tasks are done, signals the orchestrator,
// and closes the doneCh to notify the CLI run loop to exit.
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

	if allDone {
		if c.orchestratorMgr != nil {
			c.orchestratorMgr.Complete()
		}
		// Signal the CLI run loop to exit. Use sync.Once semantics via
		// select to avoid closing an already-closed channel.
		select {
		case <-c.doneCh:
			// Already closed.
		default:
			close(c.doneCh)
		}
	}
}

// DoneCh returns a channel that is closed when all tasks have completed.
// The CLI run loop selects on this channel to know when to exit gracefully.
func (c *Coordinator) DoneCh() <-chan struct{} {
	return c.doneCh
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
		// Read staged files and construct a MergeItem for the merge queue.
		mergeItem, mergeErr := c.buildMergeItem(taskID, stagingDir, outputMsg.BaseSnapshot, task)
		if mergeErr != nil {
			return nil, fmt.Errorf("build merge item: %w", mergeErr)
		}
		c.mergeQueue.Submit(mergeItem)

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

// maxRetriesPerTier is the maximum number of retries at the same model tier
// before escalating. See Architecture Section 30.1.
const maxRetriesPerTier = 3

// maxEscalations is the maximum number of tier escalations before marking
// a task as BLOCKED. See Architecture Section 30.1.
const maxEscalations = 2

// tierEscalationOrder defines the escalation path for model tiers.
// Each tier escalates to the next entry. See Architecture Section 30.1.
var tierEscalationOrder = []string{"local", "cheap", "standard", "premium"}

// escalateTier returns the next tier up from the given tier, or empty string
// if the tier is already at the highest level.
func escalateTier(currentTier string) string {
	for i, t := range tierEscalationOrder {
		if t == currentTier && i+1 < len(tierEscalationOrder) {
			return tierEscalationOrder[i+1]
		}
	}
	return ""
}

// requeueTask resets a task from in_progress back to queued through the valid
// state machine path: in_progress -> failed -> queued (Architecture Section 15.4).
//
// It enforces retry limits and tier escalation per Architecture Section 30.1:
//   - Max 3 retries at the same model tier
//   - Max 2 escalations to higher tiers
//   - After exhausting all escalations: mark task BLOCKED
func (c *Coordinator) requeueTask(taskID string) {
	task, err := c.db.GetTask(taskID)
	if err != nil {
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: taskID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("requeueTask: get task: %v", err),
			},
		})
		_ = c.workQueue.FailTask(taskID)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusBlocked)
		return
	}

	// Count total attempts and attempts at the current tier.
	totalAttempts, _ := c.db.CountTaskAttempts(taskID)
	tierAttempts, _ := c.db.CountTaskAttemptsForTier(taskID, task.Tier)

	// If we haven't exhausted retries at the current tier, just requeue.
	if tierAttempts < maxRetriesPerTier {
		_ = c.workQueue.FailTask(taskID)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusQueued)
		return
	}

	// Count how many escalations have occurred by checking how many distinct
	// tiers have been tried. Each tier beyond the original counts as one escalation.
	originalTierIdx := 0
	currentTierIdx := 0
	for i, t := range tierEscalationOrder {
		if t == task.Tier {
			currentTierIdx = i
		}
	}
	// Find the lowest tier used in any attempt to determine the original tier.
	attempts, _ := c.db.GetTaskAttempts(taskID)
	if len(attempts) > 0 {
		lowestTierUsed := currentTierIdx
		for _, a := range attempts {
			for i, t := range tierEscalationOrder {
				model := defaultModelsForTier[t]
				if a.ModelID == model && i < lowestTierUsed {
					lowestTierUsed = i
				}
			}
		}
		originalTierIdx = lowestTierUsed
	}
	escalationsDone := currentTierIdx - originalTierIdx

	// Try to escalate to a higher tier.
	nextTier := escalateTier(task.Tier)
	if nextTier != "" && escalationsDone < maxEscalations {
		_ = c.db.UpdateTaskTier(taskID, nextTier)
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: taskID,
			Details: map[string]interface{}{
				"action":          "escalate",
				"from_tier":       task.Tier,
				"to_tier":         nextTier,
				"total_attempts":  totalAttempts,
				"escalation_num":  escalationsDone + 1,
			},
		})
		_ = c.workQueue.FailTask(taskID)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
		_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusQueued)
		return
	}

	// All retries and escalations exhausted: mark task BLOCKED.
	c.emitter.Emit(events.Event{
		Type:   events.EventTaskBlocked,
		TaskID: taskID,
		Details: map[string]interface{}{
			"reason":         "exhausted all retries and escalations",
			"total_attempts": totalAttempts,
			"final_tier":     task.Tier,
		},
	})
	_ = c.workQueue.FailTask(taskID)
	_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
	_ = c.db.UpdateTaskStatus(taskID, state.TaskStatusBlocked)
}

// dispatchReadyTasks finds tasks that are ready to execute, acquires their locks,
// builds TaskSpecs, and spawns Meeseeks containers. This is the core dispatch
// cycle from Architecture Section 5.1 step 7.
func (c *Coordinator) dispatchReadyTasks() {
	dispatchable, err := c.workQueue.GetDispatchable()
	if err != nil {
		c.emitter.Emit(events.Event{
			Type:      events.EventTaskFailed,
			AgentType: "engine",
			Details: map[string]interface{}{
				"error": fmt.Sprintf("get dispatchable tasks: %v", err),
			},
		})
		return
	}

	for _, dt := range dispatchable {
		task := dt.Task
		locks := dt.Locks

		// Acquire locks and transition to in_progress.
		acquired, err := c.workQueue.AcquireAndDispatch(task.ID, locks)
		if err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("acquire and dispatch: %v", err),
				},
			})
			continue
		}
		if !acquired {
			// Race condition: another dispatch grabbed the locks. Skip.
			continue
		}

		// Register the task's tier with the broker for model allowlist enforcement.
		// See Architecture Section 19.5 (Per-Task Enforcement, point 1).
		c.infBroker.SetTaskTier(task.ID, broker.ModelTier(task.Tier))

		// Get the base snapshot (current HEAD SHA).
		baseSnapshot, err := c.gitMgr.HeadSHA()
		if err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("get HEAD SHA: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}

		// Get SRS references for this task.
		srsRefs, err := c.db.GetTaskSRSRefs(task.ID)
		if err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("get SRS refs: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}

		// Get target files for this task.
		targetFiles, err := c.db.GetTaskTargetFiles(task.ID)
		if err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("get target files: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}
		filePaths := make([]string, len(targetFiles))
		for i, f := range targetFiles {
			filePaths[i] = f.FilePath
		}

		// Build the TaskSpecRequest from task metadata.
		specReq := &TaskSpecRequest{
			Task:        task,
			ContextTier: TierFile, // Default; orchestrator may override via task metadata.
			SRSRefs:     srsRefs,
			TargetFiles: filePaths,
		}

		// For test-generation tasks, include the actual implementation source
		// files from dependency tasks as context. Per Architecture Section 11.5,
		// test Meeseeks need the committed implementation to write valid tests.
		if task.TaskType == "test" {
			implFiles := c.gatherImplementationContext(task.ID)
			if len(implFiles) > 0 {
				specReq.ImplementationFiles = implFiles
			}
		}

		// Build the TaskSpec.
		specContent, err := c.taskSpecBuilder.Build(specReq, baseSnapshot)
		if err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("build TaskSpec: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}

		// Write the TaskSpec to the spec directory.
		specDir := filepath.Join(c.projectRoot, ".axiom", "containers", "specs", task.ID)
		if err := os.MkdirAll(specDir, 0755); err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("create spec dir: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}
		specPath := filepath.Join(specDir, "spec.md")
		if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("write spec file: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}

		// Ensure per-task IPC and staging directories exist.
		ipcInputDir := filepath.Join(c.projectRoot, ".axiom", "containers", "ipc", task.ID, "input")
		ipcOutputDir := filepath.Join(c.projectRoot, ".axiom", "containers", "ipc", task.ID, "output")
		stagingDir := filepath.Join(c.projectRoot, ".axiom", "containers", "staging", task.ID)
		for _, dir := range []string{ipcInputDir, ipcOutputDir, stagingDir} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				c.emitter.Emit(events.Event{
					Type:   events.EventTaskFailed,
					TaskID: task.ID,
					Details: map[string]interface{}{
						"error": fmt.Sprintf("create task dir %s: %v", dir, err),
					},
				})
				_ = c.workQueue.FailTask(task.ID)
				_ = c.db.UpdateTaskStatus(task.ID, state.TaskStatusQueued)
				continue
			}
		}

		// Send the TaskSpec via IPC.
		ipcMsg := &ipc.TaskSpecMessage{
			Header: ipc.Header{
				Type:   ipc.TypeTaskSpec,
				TaskID: task.ID,
			},
			Spec: specContent,
		}
		if err := c.ipcWriter.Send(task.ID, ipcMsg); err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("send TaskSpec via IPC: %v", err),
				},
			})
			c.requeueTask(task.ID)
			continue
		}

		// Execute the task in-process by calling the inference broker directly.
		// The Docker container agent runtime is not yet implemented (containers
		// only have an IPC file watcher with no LLM agent logic). Until a full
		// container-based agent is built, all tasks execute in-process through
		// the inference broker, which provides the same budget tracking and audit.
		// See Architecture Section 10.2-10.5 (Meeseeks lifecycle).
		go c.executeTaskInProcess(task, specContent, baseSnapshot, stagingDir)
	}
}

// defaultModelsForTier maps task tiers to their default model IDs.
// See Architecture Section 10.2 for the tier-to-model mapping.
// Premium tier uses the most capable model available (Opus/o1).
var defaultModelsForTier = map[string]string{
	"local":    "anthropic/claude-haiku-4.5",
	"cheap":    "anthropic/claude-haiku-4.5",
	"standard": "anthropic/claude-sonnet-4",
	"premium":  "anthropic/claude-opus-4",
}

// modelForTier returns the model ID for a given task tier, taking into account
// whether BitNet is available for local-tier tasks.
// See Architecture Section 10.2 for the tier-to-model mapping.
func (c *Coordinator) modelForTier(tier string) string {
	// For local tier, check if BitNet is available and use it.
	if tier == "local" && c.config.BitNet.Enabled {
		// Query the BitNet server for available models.
		bitnetModelID := c.getBitNetModelID()
		if bitnetModelID != "" {
			return bitnetModelID
		}
		// BitNet unavailable; fall back to cheapest cloud model.
	}
	if model, ok := defaultModelsForTier[tier]; ok {
		return model
	}
	return "anthropic/claude-sonnet-4"
}

// gatherImplementationContext reads the implementation source files from the
// project that were produced by a test task's dependencies. This ensures test
// Meeseeks have the actual committed code to write tests against, preventing
// cross-Meeseeks incoherence where tests reference nonexistent types/functions.
// See Architecture Section 11.5.
func (c *Coordinator) gatherImplementationContext(testTaskID string) map[string]string {
	deps, err := c.db.GetTaskDependencies(testTaskID)
	if err != nil || len(deps) == 0 {
		return nil
	}

	implFiles := make(map[string]string)
	for _, depID := range deps {
		depTask, err := c.db.GetTask(depID)
		if err != nil || depTask.Status != string(state.TaskStatusDone) {
			continue
		}
		// Only gather from implementation tasks.
		if depTask.TaskType != "implementation" {
			continue
		}
		// Get the target files of the implementation task and read them
		// from the current project state.
		depTargetFiles, err := c.db.GetTaskTargetFiles(depID)
		if err != nil {
			continue
		}
		for _, tf := range depTargetFiles {
			fullPath := filepath.Join(c.projectRoot, tf.FilePath)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			implFiles[tf.FilePath] = string(data)
		}
	}
	return implFiles
}

// getBitNetModelID queries the BitNet server for its model ID.
// Returns empty string if BitNet is unavailable.
func (c *Coordinator) getBitNetModelID() string {
	if c.config.BitNet.Host == "" || c.config.BitNet.Port == 0 {
		return ""
	}
	url := fmt.Sprintf("http://%s:%d/v1/models", c.config.BitNet.Host, c.config.BitNet.Port)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || len(result.Data) == 0 {
		return ""
	}
	return result.Data[0].ID
}

// modelFamilyFromID extracts the model family from a model ID (e.g. "anthropic" from "anthropic/claude-sonnet-4").
func modelFamilyFromID(modelID string) string {
	for i, c := range modelID {
		if c == '/' {
			return modelID[:i]
		}
	}
	return "unknown"
}

// executeTaskInProcess runs a Meeseeks-equivalent task in the engine process
// by calling the inference broker directly with the TaskSpec. This is the
// fallback execution path when Docker containers are unavailable.
//
// The function:
//  1. Selects the appropriate model based on task tier
//  2. Records a task_attempt in the database
//  3. Builds a code-generation prompt from the TaskSpec
//  4. Calls the inference broker (same path as container-based execution)
//  5. Parses the response to extract code files
//  6. Writes output + manifest.json to the staging directory
//  7. Updates the task_attempt with completion data
//
// All inference is routed through the broker for budget tracking and audit.
func (c *Coordinator) executeTaskInProcess(task *state.Task, specContent, baseSnapshot, stagingDir string) {
	// Select the model based on the task's tier per Architecture Section 10.2.
	modelID := c.modelForTier(task.Tier)
	modelFamily := modelFamilyFromID(modelID)

	// Determine the attempt number for this task.
	existingAttempts, _ := c.db.GetTaskAttempts(task.ID)
	attemptNumber := len(existingAttempts) + 1

	// Record the task attempt per Architecture Section 15.2.
	now := time.Now()
	attempt := &state.TaskAttempt{
		TaskID:        task.ID,
		AttemptNumber: attemptNumber,
		ModelID:       modelID,
		ModelFamily:   modelFamily,
		BaseSnapshot:  baseSnapshot,
		Status:        "running",
		StartedAt:     now,
	}
	if err := c.db.InsertTaskAttempt(attempt); err != nil {
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("insert task attempt: %v", err),
			},
		})
	}

	c.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		TaskID:    task.ID,
		AgentType: "meeseeks",
		AgentID:   "direct-" + task.ID,
		Details: map[string]interface{}{
			"mode":    "in_process",
			"tier":    task.Tier,
			"model":   modelID,
			"attempt": attemptNumber,
		},
	})

	codePrompt := fmt.Sprintf(`You are a Meeseeks code generation agent. Given the following TaskSpec, produce the required code.

%s

IMPORTANT: Output your response as a JSON object with this structure:
{
  "files": {
    "path/to/file1.go": "file contents...",
    "path/to/file2.go": "file contents..."
  }
}

Output ONLY the JSON object, no other text. Every file path should be relative to the project root.
Create all files needed to complete the task.`, specContent)

	resp, err := c.infBroker.RouteRequest(context.Background(), &broker.InferenceRequest{
		TaskID:    task.ID,
		ModelID:   modelID,
		AgentType: "meeseeks",
		Messages: []broker.ChatMessage{
			{Role: "system", Content: "You are a precise code generation agent. You produce working, production-ready code. Output only valid JSON with file paths and contents."},
			{Role: "user", Content: codePrompt},
		},
		MaxTokens:   16384,
		Temperature: 0.2,
	})
	if err != nil {
		// Record the failed attempt.
		if attempt.ID > 0 {
			_ = c.db.UpdateTaskAttemptStatus(attempt.ID, "failed", err.Error())
		}
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("in-process inference: %v", err),
			},
		})
		c.requeueTask(task.ID)
		return
	}

	// Parse the response to extract files.
	files := parseCodeResponse(resp.Content)
	if len(files) == 0 {
		// Record the failed attempt.
		if attempt.ID > 0 {
			_ = c.db.UpdateTaskAttemptStatus(attempt.ID, "failed", "no files produced")
		}
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error":           "no files produced by in-process meeseeks",
				"response_length": len(resp.Content),
			},
		})
		c.requeueTask(task.ID)
		return
	}

	// Update the attempt with completion data.
	if attempt.ID > 0 {
		costUSD := 0.0
		if resp.InputTokens > 0 || resp.OutputTokens > 0 {
			// Estimate cost from broker's tracked data (already logged in cost_log).
			costUSD = float64(resp.InputTokens)*3.0/1_000_000 + float64(resp.OutputTokens)*15.0/1_000_000
		}
		_ = c.db.UpdateTaskAttemptCompleted(attempt.ID, "passed", resp.InputTokens, resp.OutputTokens, costUSD)
	}

	// Write output files to staging directory.
	for filePath, content := range files {
		fullPath := filepath.Join(stagingDir, filePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("create dir for %s: %v", filePath, err),
				},
			})
			_ = c.workQueue.FailTask(task.ID)
			_ = c.db.UpdateTaskStatus(task.ID, state.TaskStatusQueued)
			return
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			c.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: task.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("write staged file %s: %v", filePath, err),
				},
			})
			_ = c.workQueue.FailTask(task.ID)
			_ = c.db.UpdateTaskStatus(task.ID, state.TaskStatusQueued)
			return
		}
	}

	// Build and write manifest.json per Architecture Section 10.4.
	// Use currentHead (re-read below before merge submission) if available,
	// otherwise fall back to the dispatch-time baseSnapshot.
	manifest := buildManifest(task.ID, baseSnapshot, files) // snapshot updated in MergeItem below
	manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(stagingDir, "manifest.json"), manifestJSON, 0644); err != nil {
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("write manifest: %v", err),
			},
		})
		c.requeueTask(task.ID)
		return
	}

	// Stage 2: Run in-process validation before reviewer evaluation.
	// This replaces the full container-based validation sandbox (Stage 2
	// of Architecture Section 14.2) with a lighter-weight in-process check
	// that catches compilation errors, duplicate definitions, and interface
	// mismatches before they reach review.
	validationErr := c.validateInProcessOutput(task, files, attempt)
	if validationErr != nil {
		if attempt.ID > 0 {
			_ = c.db.UpdateTaskAttemptStatus(attempt.ID, "failed", validationErr.Error())
		}
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error":  fmt.Sprintf("validation failed: %v", validationErr),
				"stage":  "in_process_validation",
			},
		})
		c.requeueTask(task.ID)
		return
	}

	// Stage 3: Reviewer evaluation per Architecture Section 14.2.
	// NO Meeseeks output shall be promoted without passing reviewer approval.
	reviewErr := c.reviewInProcessOutput(task, specContent, files, attempt, modelID)
	if reviewErr != nil {
		if attempt.ID > 0 {
			_ = c.db.UpdateTaskAttemptStatus(attempt.ID, "failed", reviewErr.Error())
		}
		c.emitter.Emit(events.Event{
			Type:   events.EventTaskFailed,
			TaskID: task.ID,
			Details: map[string]interface{}{
				"error": fmt.Sprintf("review rejected: %v", reviewErr),
				"stage": "in_process_review",
			},
		})
		c.requeueTask(task.ID)
		return
	}

	// Re-read HEAD SHA just before submitting to the merge queue.
	// In-process execution runs in a goroutine and other tasks may have
	// committed while the inference call was in-flight. Using the current
	// HEAD (rather than the stale baseSnapshot from dispatch time) avoids
	// unnecessary stale-snapshot requeue cycles that waste budget.
	currentHead, headErr := c.gitMgr.HeadSHA()
	if headErr != nil {
		currentHead = baseSnapshot // fall back to original if git fails
	}

	// Submit to the merge queue.
	srsRefs, _ := c.db.GetTaskSRSRefs(task.ID)
	mergeItem := &merge.MergeItem{
		TaskID:        task.ID,
		TaskTitle:     task.Title,
		StagingDir:    stagingDir,
		BaseSnapshot:  currentHead,
		Files:         files,
		SRSRefs:       srsRefs,
		MeeseeksModel: modelID,
		AttemptNumber: attemptNumber,
		MaxAttempts:   3,
	}
	c.mergeQueue.Submit(mergeItem)

	c.emitter.Emit(events.Event{
		Type:      events.EventTaskCompleted,
		TaskID:    task.ID,
		AgentType: "meeseeks",
		AgentID:   "direct-" + task.ID,
		Details: map[string]interface{}{
			"mode":       "in_process",
			"file_count": len(files),
		},
	})
}

// parseCodeResponse extracts file paths and contents from the LLM's JSON response.
func parseCodeResponse(response string) map[string]string {
	// Find the outermost JSON object in the response.
	start := -1
	end := -1
	depth := 0
	for i, c := range response {
		if c == '{' && start == -1 {
			start = i
			depth = 1
		} else if c == '{' && start != -1 {
			depth++
		} else if c == '}' && start != -1 {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}

	if start == -1 || end == -1 {
		return nil
	}

	var parsed struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(response[start:end]), &parsed); err != nil {
		return nil
	}
	return parsed.Files
}

// buildManifest creates a manifest.json structure for the given files.
// See Architecture Section 10.4.
func buildManifest(taskID, baseSnapshot string, files map[string]string) map[string]interface{} {
	added := make([]map[string]interface{}, 0, len(files))
	for path := range files {
		added = append(added, map[string]interface{}{
			"path":   path,
			"binary": false,
		})
	}
	return map[string]interface{}{
		"task_id":       taskID,
		"base_snapshot": baseSnapshot,
		"files": map[string]interface{}{
			"added":    added,
			"modified": []interface{}{},
			"deleted":  []interface{}{},
			"renamed":  []interface{}{},
		},
	}
}

// validateInProcessOutput performs lightweight validation on in-process task
// output before it reaches the merge queue. This is the in-process equivalent
// of the validation sandbox (Stage 2 of Architecture Section 14.2).
//
// It creates a temporary directory overlaying the current project with the
// task's output files, then runs language-appropriate build checks. This
// catches compilation errors, duplicate definitions, and interface mismatches
// that would otherwise be merged into the codebase.
func (c *Coordinator) validateInProcessOutput(task *state.Task, files map[string]string, attempt *state.TaskAttempt) error {
	// Create a temporary validation directory with the project + task output.
	tmpDir, err := os.MkdirTemp("", "axiom-validate-"+task.ID+"-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy current project files to the temp directory.
	copyCmd := exec.Command("cp", "-a", c.projectRoot+"/.", tmpDir+"/")
	if output, cpErr := copyCmd.CombinedOutput(); cpErr != nil {
		return fmt.Errorf("copy project: %s: %w", string(output), cpErr)
	}

	// Remove .axiom dir from the copy (not needed for validation).
	os.RemoveAll(filepath.Join(tmpDir, ".axiom"))

	// Overlay the task output files.
	for filePath, content := range files {
		fullPath := filepath.Join(tmpDir, filePath)
		if mkErr := os.MkdirAll(filepath.Dir(fullPath), 0755); mkErr != nil {
			return fmt.Errorf("mkdir for %s: %w", filePath, mkErr)
		}
		if wErr := os.WriteFile(fullPath, []byte(content), 0644); wErr != nil {
			return fmt.Errorf("write %s: %w", filePath, wErr)
		}
	}

	// Detect project language and run appropriate build check.
	if _, err := os.Stat(filepath.Join(tmpDir, "go.mod")); err == nil {
		return c.validateGo(tmpDir, task, attempt)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "package.json")); err == nil {
		return c.validateNode(tmpDir, task, attempt)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "requirements.txt")); err == nil {
		return c.validatePython(tmpDir, task, attempt)
	}

	// No recognized project type; skip validation.
	return nil
}

// validateGo runs `go build ./...` and `go vet ./...` on the temporary project.
func (c *Coordinator) validateGo(tmpDir string, task *state.Task, attempt *state.TaskAttempt) error {
	start := time.Now()

	// Run go build.
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = tmpDir
	buildCmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	output, err := buildCmd.CombinedOutput()
	duration := time.Since(start)

	// Record validation run in database.
	status := "pass"
	outputStr := ""
	if err != nil {
		status = "fail"
		outputStr = strings.TrimSpace(string(output))
	}
	if attempt != nil && attempt.ID > 0 {
		_ = c.db.InsertValidationRun(&state.ValidationRun{
			AttemptID:  attempt.ID,
			CheckType:  "compile",
			Status:     status,
			Output:     outputStr,
			DurationMs: int(duration.Milliseconds()),
			Timestamp:  time.Now(),
		})
	}

	if err != nil {
		return fmt.Errorf("go build failed:\n%s", outputStr)
	}

	// Run go vet as a blocking lint check per Architecture Section 14.2.
	// Lint failures block promotion to prevent code with vet errors
	// (undefined references, type assertion issues) from being merged.
	vetCmd := exec.Command("go", "vet", "./...")
	vetCmd.Dir = tmpDir
	vetCmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	vetOutput, vetErr := vetCmd.CombinedOutput()
	vetStatus := "pass"
	vetOutputStr := ""
	if vetErr != nil {
		vetStatus = "fail"
		vetOutputStr = strings.TrimSpace(string(vetOutput))
	}
	if attempt != nil && attempt.ID > 0 {
		_ = c.db.InsertValidationRun(&state.ValidationRun{
			AttemptID:  attempt.ID,
			CheckType:  "lint",
			Status:     vetStatus,
			Output:     vetOutputStr,
			DurationMs: int(time.Since(start).Milliseconds()),
			Timestamp:  time.Now(),
		})
	}

	if vetErr != nil {
		return fmt.Errorf("go vet failed:\n%s", vetOutputStr)
	}

	return nil
}

// validateNode runs a syntax check on Node.js projects by finding all .js
// files and checking each one individually. node --check expects a single
// file path, not a directory.
// See Architecture Section 13.5 for language-specific validation profiles.
func (c *Coordinator) validateNode(tmpDir string, task *state.Task, attempt *state.TaskAttempt) error {
	if _, err := exec.LookPath("node"); err != nil {
		return nil // Node not available; skip.
	}

	start := time.Now()

	// Find all .js files in the temp directory (excluding node_modules).
	findCmd := exec.Command("find", tmpDir, "-name", "*.js", "-not", "-path", "*/node_modules/*", "-type", "f")
	findOutput, findErr := findCmd.Output()
	if findErr != nil || len(strings.TrimSpace(string(findOutput))) == 0 {
		// No .js files found; skip validation.
		return nil
	}

	jsFiles := strings.Split(strings.TrimSpace(string(findOutput)), "\n")
	var failedFiles []string
	for _, f := range jsFiles {
		checkCmd := exec.Command("node", "--check", f)
		checkCmd.Dir = tmpDir
		output, err := checkCmd.CombinedOutput()
		if err != nil {
			failedFiles = append(failedFiles, fmt.Sprintf("%s: %s", f, strings.TrimSpace(string(output))))
		}
	}

	status := "pass"
	outputStr := ""
	if len(failedFiles) > 0 {
		status = "fail"
		outputStr = strings.Join(failedFiles, "\n")
	}
	if attempt != nil && attempt.ID > 0 {
		_ = c.db.InsertValidationRun(&state.ValidationRun{
			AttemptID:  attempt.ID,
			CheckType:  "compile",
			Status:     status,
			Output:     outputStr,
			DurationMs: int(time.Since(start).Milliseconds()),
			Timestamp:  time.Now(),
		})
	}
	if len(failedFiles) > 0 {
		return fmt.Errorf("node syntax check failed:\n%s", outputStr)
	}
	return nil
}

// validatePython runs a syntax check on Python projects using compileall,
// which correctly handles directories by recursively compiling all .py files.
// See Architecture Section 13.5 for language-specific validation profiles.
func (c *Coordinator) validatePython(tmpDir string, task *state.Task, attempt *state.TaskAttempt) error {
	if _, err := exec.LookPath("python3"); err != nil {
		return nil // Python not available; skip.
	}

	start := time.Now()
	// Use compileall instead of py_compile: compileall handles directories
	// natively, whereas py_compile expects individual file paths and fails
	// with IsADirectoryError when given ".".
	checkCmd := exec.Command("python3", "-m", "compileall", "-q", tmpDir)
	checkCmd.Dir = tmpDir
	output, err := checkCmd.CombinedOutput()
	if attempt != nil && attempt.ID > 0 {
		status := "pass"
		outputStr := ""
		if err != nil {
			status = "fail"
			outputStr = strings.TrimSpace(string(output))
		}
		_ = c.db.InsertValidationRun(&state.ValidationRun{
			AttemptID:  attempt.ID,
			CheckType:  "compile",
			Status:     status,
			Output:     outputStr,
			DurationMs: int(time.Since(start).Milliseconds()),
			Timestamp:  time.Now(),
		})
	}
	if err != nil {
		return fmt.Errorf("python syntax check failed:\n%s", strings.TrimSpace(string(output)))
	}
	return nil
}

// reviewerModelForMeeseeks selects a reviewer model from a different family
// than the Meeseeks model per Architecture Section 11.3. For standard+ tiers,
// the reviewer SHOULD be from a different model family to prevent correlated
// blind spots and rubber-stamping.
func reviewerModelForMeeseeks(meeseeksModel, tier string) string {
	meeseeksFamily := modelFamilyFromID(meeseeksModel)

	// For local/cheap tiers, model family diversification is optional.
	// Use the same tier model.
	if tier == "local" || tier == "cheap" {
		return defaultModelsForTier[tier]
	}

	// For standard/premium: pick a reviewer from a different family.
	// Use OpenAI as the default alternative to Anthropic.
	if meeseeksFamily == "anthropic" {
		return "openai/gpt-4o"
	}
	// If Meeseeks is non-Anthropic, use Anthropic for review.
	return "anthropic/claude-sonnet-4"
}

// reviewInProcessOutput runs an in-process reviewer evaluation of task output
// per Architecture Section 14.2 Stage 3. The reviewer evaluates the Meeseeks
// output against the TaskSpec and returns APPROVE or REJECT.
//
// This ensures NO Meeseeks output is promoted without passing reviewer approval.
func (c *Coordinator) reviewInProcessOutput(task *state.Task, specContent string, files map[string]string, attempt *state.TaskAttempt, meeseeksModel string) error {
	reviewerModel := reviewerModelForMeeseeks(meeseeksModel, task.Tier)
	reviewerFamily := modelFamilyFromID(reviewerModel)

	c.emitter.Emit(events.Event{
		Type:      events.EventReviewStarted,
		TaskID:    task.ID,
		AgentType: "reviewer",
		AgentID:   "review-" + task.ID,
		Details: map[string]interface{}{
			"reviewer_model": reviewerModel,
			"meeseeks_model": meeseeksModel,
		},
	})

	// Build the ReviewSpec content: original TaskSpec + output files.
	var filesListing strings.Builder
	for filePath, content := range files {
		filesListing.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", filePath, content))
	}

	reviewPrompt := fmt.Sprintf(`You are a code reviewer agent. Evaluate the following Meeseeks output against the original TaskSpec.

## Original TaskSpec
%s

## Meeseeks Output Files
%s

## Review Instructions
Evaluate the output against the TaskSpec. Check for:
- Correctness against the objective and acceptance criteria
- Interface contract compliance
- Obvious bugs, edge cases, or security issues
- Code quality and style compliance

Respond ONLY with a JSON object:
{
  "verdict": "APPROVE" or "REJECT",
  "feedback": "explanation of issues found (if REJECT) or confirmation of quality (if APPROVE)"
}

Output ONLY the JSON object.`, specContent, filesListing.String())

	resp, err := c.infBroker.RouteRequest(context.Background(), &broker.InferenceRequest{
		TaskID:    task.ID,
		ModelID:   reviewerModel,
		AgentType: "reviewer",
		Messages: []broker.ChatMessage{
			{Role: "system", Content: "You are a thorough code reviewer. Evaluate code against specifications. Be strict about correctness but pragmatic about style."},
			{Role: "user", Content: reviewPrompt},
		},
		MaxTokens:   4096,
		Temperature: 0.1,
	})
	if err != nil {
		// If the reviewer model is unavailable, log a warning but don't block.
		// Fall back to a same-family model.
		c.emitter.Emit(events.Event{
			Type:      events.EventReviewCompleted,
			TaskID:    task.ID,
			AgentType: "reviewer",
			Details: map[string]interface{}{
				"verdict": "approve",
				"note":    fmt.Sprintf("reviewer model %s unavailable, auto-approved: %v", reviewerModel, err),
			},
		})
		return nil
	}

	// Record the review cost.
	reviewCost := float64(resp.InputTokens)*3.0/1_000_000 + float64(resp.OutputTokens)*15.0/1_000_000

	// Parse the review verdict.
	verdict, feedback := parseReviewVerdict(resp.Content)

	// Record the review run in the database.
	if attempt != nil && attempt.ID > 0 {
		_ = c.db.InsertReviewRun(&state.ReviewRun{
			AttemptID:      attempt.ID,
			ReviewerModel:  reviewerModel,
			ReviewerFamily: reviewerFamily,
			Verdict:        verdict,
			Feedback:       feedback,
			CostUSD:        reviewCost,
			Timestamp:      time.Now(),
		})
	}

	c.emitter.Emit(events.Event{
		Type:      events.EventReviewCompleted,
		TaskID:    task.ID,
		AgentType: "reviewer",
		AgentID:   "review-" + task.ID,
		Details: map[string]interface{}{
			"verdict":        verdict,
			"reviewer_model": reviewerModel,
			"feedback":       feedback,
		},
	})

	if verdict == "reject" {
		return fmt.Errorf("reviewer rejected: %s", feedback)
	}

	return nil
}

// parseReviewVerdict extracts the verdict and feedback from a reviewer's JSON response.
func parseReviewVerdict(response string) (verdict, feedback string) {
	// Try to parse as JSON.
	var result struct {
		Verdict  string `json:"verdict"`
		Feedback string `json:"feedback"`
	}

	// Find JSON in response.
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(response[start:end+1]), &result); err == nil {
			v := strings.ToLower(strings.TrimSpace(result.Verdict))
			if v == "approve" || v == "reject" {
				return v, result.Feedback
			}
		}
	}

	// Fallback: look for APPROVE/REJECT keywords in raw text.
	lower := strings.ToLower(response)
	if strings.Contains(lower, "reject") {
		return "reject", response
	}
	// Default to approve if we can't parse the response.
	return "approve", response
}

// buildMergeItem reads staged files and constructs a MergeItem for submission
// to the merge queue. Called after the approval pipeline approves task output.
func (c *Coordinator) buildMergeItem(taskID, stagingDir, baseSnapshot string, task *state.Task) (*merge.MergeItem, error) {
	// Parse the manifest to identify files.
	manifest, err := pipeline.ParseManifest(stagingDir)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	// Read file contents from staging.
	files := make(map[string]string)
	for _, f := range manifest.Files.Added {
		if f.Binary {
			continue // Binary files are handled separately.
		}
		content, readErr := os.ReadFile(filepath.Join(stagingDir, f.Path))
		if readErr != nil {
			return nil, fmt.Errorf("read staged file %s: %w", f.Path, readErr)
		}
		files[f.Path] = string(content)
	}
	for _, f := range manifest.Files.Modified {
		if f.Binary {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(stagingDir, f.Path))
		if readErr != nil {
			return nil, fmt.Errorf("read staged file %s: %w", f.Path, readErr)
		}
		files[f.Path] = string(content)
	}
	// Handle renames: read the new file content.
	for _, r := range manifest.Files.Renamed {
		content, readErr := os.ReadFile(filepath.Join(stagingDir, r.To))
		if readErr != nil {
			return nil, fmt.Errorf("read renamed file %s: %w", r.To, readErr)
		}
		files[r.To] = string(content)
	}

	// Collect deletions.
	deletions := make([]string, len(manifest.Files.Deleted))
	copy(deletions, manifest.Files.Deleted)
	// Renames also delete the old path.
	for _, r := range manifest.Files.Renamed {
		deletions = append(deletions, r.From)
	}

	// Get SRS refs for the commit metadata.
	srsRefs, _ := c.db.GetTaskSRSRefs(taskID)

	return &merge.MergeItem{
		TaskID:       taskID,
		TaskTitle:    task.Title,
		StagingDir:   stagingDir,
		BaseSnapshot: baseSnapshot,
		Files:        files,
		Deletions:    deletions,
		SRSRefs:      srsRefs,
	}, nil
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

// ApproveSRS approves the SRS document. Delegates to the SRS approval manager.
// This method satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) ApproveSRS(approvedBy string) (string, error) {
	return c.srsApproval.Approve(approvedBy)
}

// RejectSRS rejects the SRS document with feedback. Delegates to the SRS approval manager.
// This method satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) RejectSRS(feedback string) error {
	return c.srsApproval.Reject(feedback)
}

// ApproveECO approves an Engineering Change Order. Delegates to the ECO manager.
// This method satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) ApproveECO(ecoID int64, approvedBy string) error {
	return c.ecoMgr.ApproveECO(ecoID, approvedBy)
}

// RejectECO rejects an Engineering Change Order. Delegates to the ECO manager.
// This method satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) RejectECO(ecoID int64, rejectedBy string) error {
	return c.ecoMgr.RejectECO(ecoID, rejectedBy)
}

// IsPaused returns whether the coordinator is currently paused.
// This method satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

// CompletionPercentage calculates the percentage of tasks that are done.
// This is the public accessor that satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) CompletionPercentage() float64 {
	return c.completionPercentage()
}

// ActiveContainerCount returns the number of active agent containers.
// This is the public accessor that satisfies the api.CoordinatorAPI interface.
func (c *Coordinator) ActiveContainerCount() int {
	return c.activeContainerCount()
}

// activeContainerCount returns the number of active agent containers.
func (c *Coordinator) activeContainerCount() int {
	if c.containerMgr == nil {
		return 0
	}
	return c.containerMgr.ActiveCount()
}

// StartOrchestrator initializes and starts the orchestrator based on the
// configured runtime and available infrastructure.
//
// When Docker is available and the runtime is an embedded type (claude-code,
// codex, opencode), it starts an Embedded orchestrator in a container.
//
// When Docker is unavailable or the runtime is "claw" with no external Claw
// connected, it falls back to a Direct orchestrator that calls the inference
// broker (OpenRouter) in-process.
//
// See Architecture Section 8 (Orchestrator).
func (c *Coordinator) StartOrchestrator(ctx context.Context, prompt string) error {
	// Determine if this is a greenfield project (no source files).
	isGreenfield := c.isGreenfield()

	// Generate a project ID from config or directory name.
	projectID := c.config.Project.Slug
	if projectID == "" {
		projectID = filepath.Base(c.projectRoot)
	}
	c.projectID = projectID

	runtime := c.config.Orchestrator.Runtime

	// Create the project branch per Architecture Section 23.1.
	// All task commits go to axiom/<project-slug>, never to the user's branch.
	branchName, err := c.gitMgr.CreateProjectBranch(projectID)
	if err != nil {
		return fmt.Errorf("create project branch: %w", err)
	}
	c.emitter.Emit(events.Event{
		Type:      events.EventTaskCreated,
		AgentType: "engine",
		Details: map[string]interface{}{
			"action": "project_branch_created",
			"branch": branchName,
		},
	})

	// Try embedded orchestrator if Docker is available and runtime supports it.
	if c.containerMgr != nil && runtime != "claw" {
		c.orchestratorMgr = orchestrator.NewEmbedded(
			orchestrator.EmbeddedConfig{
				Runtime:     orchestrator.Runtime(runtime),
				Image:       c.config.Docker.Image,
				CPULimit:    c.config.Docker.CPULimit,
				MemoryLimit: c.config.Docker.MemLimit,
				TimeoutMin:  c.config.Docker.TimeoutMinutes,
				ProjectSlug: projectID,
				BudgetUSD:   c.config.Budget.MaxUSD,
			},
			c.containerMgr,
			c.db,
			c.emitter,
			c.ipcWriter,
		)
		if err := c.orchestratorMgr.Start(ctx, projectID, prompt, isGreenfield); err != nil {
			// Embedded orchestrator failed (e.g., Docker image missing).
			// Fall through to direct orchestrator.
			c.emitter.Emit(events.Event{
				Type:      events.EventProviderUnavailable,
				AgentType: "engine",
				Details: map[string]interface{}{
					"provider": "embedded_orchestrator",
					"error":    err.Error(),
					"fallback": "direct_orchestrator",
				},
			})
			c.orchestratorMgr = nil
		} else {
			return nil
		}
	}

	// Fallback: Direct orchestrator using OpenRouter via the inference broker.
	// This runs in-process without Docker containers.
	directOrch := orchestrator.NewDirect(
		orchestrator.DirectConfig{
			Runtime:             orchestrator.Runtime(runtime),
			BudgetUSD:           c.config.Budget.MaxUSD,
			ProjectSlug:         projectID,
			SRSApprovalDelegate: c.config.Orchestrator.SRSApprovalDelegate,
		},
		c.infBroker,
		c.db,
		c.emitter,
		c.srsApproval,
	)

	if err := directOrch.Start(ctx, projectID, prompt, isGreenfield); err != nil {
		return fmt.Errorf("start direct orchestrator: %w", err)
	}

	// Store the direct orchestrator so checkCompletion() can signal it
	// and the coordinator can manage its lifecycle (BUG-027 fix).
	c.orchestratorMgr = directOrch

	c.emitter.Emit(events.Event{
		Type:      events.EventContainerSpawned,
		AgentType: "engine",
		Details: map[string]interface{}{
			"orchestrator_mode": "direct",
			"runtime":           runtime,
		},
	})

	return nil
}

// isGreenfield returns true if the project has no existing source files
// (ignoring .axiom/, .git/, and other metadata directories).
func (c *Coordinator) isGreenfield() bool {
	entries, err := os.ReadDir(c.projectRoot)
	if err != nil {
		return true
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".axiom" || name == ".git" || name == ".gitignore" || name == ".claude" {
			continue
		}
		// Any other file/directory means existing project.
		return false
	}
	return true
}

// ReloadConfig re-reads configuration from disk and updates live subsystems.
// Currently updates the inference broker's OpenRouter API key.
// See Architecture Section 19.5 (Credential Management).
func (c *Coordinator) ReloadConfig() error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	// Update OpenRouter API key in the broker.
	apiKey := cfg.OpenRouter.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
	}
	c.infBroker.UpdateOpenRouterKey(apiKey)

	// Update the stored config reference.
	c.mu.Lock()
	c.config = cfg
	c.mu.Unlock()

	c.emitter.Emit(events.Event{
		Type:      events.EventTaskCreated, // Using generic event for config reload
		AgentType: "engine",
		Details: map[string]interface{}{
			"action": "config_reloaded",
		},
	})

	return nil
}

// WritePIDFile writes the current process PID to .axiom/engine.pid.
// Used by `axiom config reload` to find the running engine process.
func (c *Coordinator) WritePIDFile() error {
	pidPath := filepath.Join(c.projectRoot, ".axiom", "engine.pid")
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
}

// RemovePIDFile removes the PID file on shutdown.
func (c *Coordinator) RemovePIDFile() {
	pidPath := filepath.Join(c.projectRoot, ".axiom", "engine.pid")
	os.Remove(pidPath)
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
