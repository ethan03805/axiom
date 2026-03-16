package orchestrator

import (
	"encoding/json"
	"fmt"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

// ActionHandler processes action_request IPC messages from the orchestrator
// container. Each action is validated against current state and policy,
// then executed by the engine.
//
// See Architecture Section 8.6 for the orchestrator-engine contract.
type ActionHandler struct {
	db      *state.DB
	emitter *events.Emitter

	// Callbacks wired by the engine for privileged operations.
	OnSubmitSRS          func(taskID, srsContent string) error
	OnSubmitECO          func(taskID, ecoCode, category, description, affectedRefs, proposedChange string) error
	OnCreateTask         func(task *state.Task) error
	OnCreateTaskBatch    func(tasks []*state.Task) error
	OnSpawnMeeseeks      func(taskID, modelID string) error
	OnSpawnReviewer      func(taskID, modelID string) error
	OnSpawnSubOrch       func(taskID, modelID string) error
	OnApproveOutput      func(taskID string) error
	OnRejectOutput       func(taskID, feedback string) error
	OnQueryStatus        func() (interface{}, error)
	OnQueryBudget        func() (interface{}, error)
}

// NewActionHandler creates an ActionHandler.
func NewActionHandler(db *state.DB, emitter *events.Emitter) *ActionHandler {
	return &ActionHandler{
		db:      db,
		emitter: emitter,
	}
}

// HandleAction processes an action_request IPC message. Routes the action
// to the appropriate callback and returns a response.
//
// Designed to be registered with the IPC Dispatcher for TypeActionRequest.
func (h *ActionHandler) HandleAction(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	reqMsg, ok := msg.(*ipc.ActionRequestMessage)
	if !ok {
		return h.errorResponse(taskID, "invalid_action", "expected ActionRequestMessage"), nil
	}

	h.emitter.Emit(events.Event{
		Type:      events.EventTaskCreated, // Using generic event; a dedicated type could be added
		TaskID:    taskID,
		AgentType: "orchestrator",
		Details: map[string]interface{}{
			"action": reqMsg.Action,
		},
	})

	switch reqMsg.Action {
	case "submit_srs":
		return h.handleSubmitSRS(taskID, reqMsg.Parameters)
	case "submit_eco":
		return h.handleSubmitECO(taskID, reqMsg.Parameters)
	case "create_task":
		return h.handleCreateTask(taskID, reqMsg.Parameters)
	case "create_task_batch":
		return h.handleCreateTaskBatch(taskID, reqMsg.Parameters)
	case "spawn_meeseeks":
		return h.handleSpawn(taskID, reqMsg.Parameters, "meeseeks")
	case "spawn_reviewer":
		return h.handleSpawn(taskID, reqMsg.Parameters, "reviewer")
	case "spawn_sub_orchestrator":
		return h.handleSpawn(taskID, reqMsg.Parameters, "sub_orchestrator")
	case "approve_output":
		return h.handleApproveOutput(taskID, reqMsg.Parameters)
	case "reject_output":
		return h.handleRejectOutput(taskID, reqMsg.Parameters)
	case "query_status":
		return h.handleQueryStatus(taskID)
	case "query_budget":
		return h.handleQueryBudget(taskID)
	default:
		return h.errorResponse(taskID, reqMsg.Action, fmt.Sprintf("unknown action: %s", reqMsg.Action)), nil
	}
}

func (h *ActionHandler) handleSubmitSRS(taskID string, params json.RawMessage) (interface{}, error) {
	var p struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return h.errorResponse(taskID, "submit_srs", "invalid parameters"), nil
	}
	if h.OnSubmitSRS != nil {
		if err := h.OnSubmitSRS(taskID, p.Content); err != nil {
			return h.errorResponse(taskID, "submit_srs", err.Error()), nil
		}
	}
	return h.successResponse(taskID, "submit_srs", map[string]interface{}{"status": "submitted"}), nil
}

func (h *ActionHandler) handleSubmitECO(taskID string, params json.RawMessage) (interface{}, error) {
	var p struct {
		EcoCode        string `json:"eco_code"`
		Category       string `json:"category"`
		Description    string `json:"description"`
		AffectedRefs   string `json:"affected_refs"`
		ProposedChange string `json:"proposed_change"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return h.errorResponse(taskID, "submit_eco", "invalid parameters"), nil
	}
	if h.OnSubmitECO != nil {
		if err := h.OnSubmitECO(taskID, p.EcoCode, p.Category, p.Description, p.AffectedRefs, p.ProposedChange); err != nil {
			return h.errorResponse(taskID, "submit_eco", err.Error()), nil
		}
	}
	return h.successResponse(taskID, "submit_eco", map[string]interface{}{"status": "proposed"}), nil
}

func (h *ActionHandler) handleCreateTask(taskID string, params json.RawMessage) (interface{}, error) {
	var task state.Task
	if err := json.Unmarshal(params, &task); err != nil {
		return h.errorResponse(taskID, "create_task", "invalid parameters"), nil
	}
	if h.OnCreateTask != nil {
		if err := h.OnCreateTask(&task); err != nil {
			return h.errorResponse(taskID, "create_task", err.Error()), nil
		}
	} else {
		if err := h.db.CreateTask(&task); err != nil {
			return h.errorResponse(taskID, "create_task", err.Error()), nil
		}
	}
	return h.successResponse(taskID, "create_task", map[string]interface{}{"task_id": task.ID}), nil
}

func (h *ActionHandler) handleCreateTaskBatch(taskID string, params json.RawMessage) (interface{}, error) {
	var tasks []*state.Task
	if err := json.Unmarshal(params, &tasks); err != nil {
		return h.errorResponse(taskID, "create_task_batch", "invalid parameters"), nil
	}
	if h.OnCreateTaskBatch != nil {
		if err := h.OnCreateTaskBatch(tasks); err != nil {
			return h.errorResponse(taskID, "create_task_batch", err.Error()), nil
		}
	} else {
		if err := h.db.CreateTaskBatch(tasks); err != nil {
			return h.errorResponse(taskID, "create_task_batch", err.Error()), nil
		}
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return h.successResponse(taskID, "create_task_batch", map[string]interface{}{"task_ids": ids}), nil
}

func (h *ActionHandler) handleSpawn(taskID string, params json.RawMessage, spawnType string) (interface{}, error) {
	var p struct {
		TaskID  string `json:"task_id"`
		ModelID string `json:"model_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return h.errorResponse(taskID, "spawn_"+spawnType, "invalid parameters"), nil
	}

	var spawnFn func(string, string) error
	switch spawnType {
	case "meeseeks":
		spawnFn = h.OnSpawnMeeseeks
	case "reviewer":
		spawnFn = h.OnSpawnReviewer
	case "sub_orchestrator":
		spawnFn = h.OnSpawnSubOrch
	}

	if spawnFn != nil {
		if err := spawnFn(p.TaskID, p.ModelID); err != nil {
			return h.errorResponse(taskID, "spawn_"+spawnType, err.Error()), nil
		}
	}
	return h.successResponse(taskID, "spawn_"+spawnType, map[string]interface{}{
		"task_id": p.TaskID,
		"status":  "spawned",
	}), nil
}

func (h *ActionHandler) handleApproveOutput(taskID string, params json.RawMessage) (interface{}, error) {
	var p struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return h.errorResponse(taskID, "approve_output", "invalid parameters"), nil
	}
	if h.OnApproveOutput != nil {
		if err := h.OnApproveOutput(p.TaskID); err != nil {
			return h.errorResponse(taskID, "approve_output", err.Error()), nil
		}
	}
	return h.successResponse(taskID, "approve_output", map[string]interface{}{"task_id": p.TaskID}), nil
}

func (h *ActionHandler) handleRejectOutput(taskID string, params json.RawMessage) (interface{}, error) {
	var p struct {
		TaskID   string `json:"task_id"`
		Feedback string `json:"feedback"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return h.errorResponse(taskID, "reject_output", "invalid parameters"), nil
	}
	if h.OnRejectOutput != nil {
		if err := h.OnRejectOutput(p.TaskID, p.Feedback); err != nil {
			return h.errorResponse(taskID, "reject_output", err.Error()), nil
		}
	}
	return h.successResponse(taskID, "reject_output", map[string]interface{}{"task_id": p.TaskID}), nil
}

func (h *ActionHandler) handleQueryStatus(taskID string) (interface{}, error) {
	if h.OnQueryStatus != nil {
		result, err := h.OnQueryStatus()
		if err != nil {
			return h.errorResponse(taskID, "query_status", err.Error()), nil
		}
		return h.successResponse(taskID, "query_status", result), nil
	}
	// Default: return task tree summary from DB.
	tasks, err := h.db.ListTasks(state.TaskFilter{})
	if err != nil {
		return h.errorResponse(taskID, "query_status", err.Error()), nil
	}
	return h.successResponse(taskID, "query_status", map[string]interface{}{"tasks": tasks}), nil
}

func (h *ActionHandler) handleQueryBudget(taskID string) (interface{}, error) {
	if h.OnQueryBudget != nil {
		result, err := h.OnQueryBudget()
		if err != nil {
			return h.errorResponse(taskID, "query_budget", err.Error()), nil
		}
		return h.successResponse(taskID, "query_budget", result), nil
	}
	total, err := h.db.GetProjectCost()
	if err != nil {
		return h.errorResponse(taskID, "query_budget", err.Error()), nil
	}
	return h.successResponse(taskID, "query_budget", map[string]interface{}{"total_cost_usd": total}), nil
}

func (h *ActionHandler) successResponse(taskID, action string, result interface{}) *ipc.ActionResponseMessage {
	resultJSON, _ := json.Marshal(result)
	return &ipc.ActionResponseMessage{
		Header:  ipc.Header{Type: ipc.TypeActionResponse, TaskID: taskID},
		Action:  action,
		Success: true,
		Result:  resultJSON,
	}
}

func (h *ActionHandler) errorResponse(taskID, action, errMsg string) *ipc.ActionResponseMessage {
	return &ipc.ActionResponseMessage{
		Header:  ipc.Header{Type: ipc.TypeActionResponse, TaskID: taskID},
		Action:  action,
		Success: false,
		Error:   errMsg,
	}
}
