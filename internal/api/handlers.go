package api

import (
	"encoding/json"
	"net/http"

	"github.com/ethan03805/axiom/internal/events"
)

// Handlers implements all REST endpoint handlers.
// Each handler maps an API request to an engine operation.
// See Architecture Section 24.2.
type Handlers struct {
	emitter *events.Emitter

	// Callbacks wired by the engine for actual operations.
	OnCreateProject func(prompt string, budget float64) (string, error)
	OnRunProject    func(projectID, prompt string) error
	OnApproveSRS    func(projectID string) error
	OnRejectSRS     func(projectID, feedback string) error
	OnApproveECO    func(projectID string, ecoID int64) error
	OnRejectECO     func(projectID string, ecoID int64) error
	OnPause         func(projectID string) error
	OnResume        func(projectID string) error
	OnCancel        func(projectID string) error
	OnGetStatus     func(projectID string) (interface{}, error)
	OnGetTasks      func(projectID string) (interface{}, error)
	OnGetAttempts   func(projectID, taskID string) (interface{}, error)
	OnGetCosts      func(projectID string) (interface{}, error)
	OnGetEvents     func(projectID string) (interface{}, error)
	OnGetModels     func() (interface{}, error)
	OnQueryIndex    func(queryType string, params map[string]string) (interface{}, error)
}

func (h *Handlers) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string  `json:"prompt"`
		Budget float64 `json:"budget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if h.OnCreateProject != nil {
		id, err := h.OnCreateProject(req.Prompt, req.Budget)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"project_id": id})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

func (h *Handlers) RunProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var req struct {
		Prompt string `json:"prompt"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if h.OnRunProject != nil {
		if err := h.OnRunProject(projectID, req.Prompt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
}

func (h *Handlers) ApproveSRS(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnApproveSRS != nil {
		if err := h.OnApproveSRS(projectID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (h *Handlers) RejectSRS(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var req struct {
		Feedback string `json:"feedback"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if h.OnRejectSRS != nil {
		if err := h.OnRejectSRS(projectID, req.Feedback); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
}

func (h *Handlers) ApproveECO(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var req struct {
		ECOID int64 `json:"eco_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if h.OnApproveECO != nil {
		if err := h.OnApproveECO(projectID, req.ECOID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "eco_approved"})
}

func (h *Handlers) RejectECO(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var req struct {
		ECOID int64 `json:"eco_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if h.OnRejectECO != nil {
		if err := h.OnRejectECO(projectID, req.ECOID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "eco_rejected"})
}

func (h *Handlers) PauseProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnPause != nil {
		h.OnPause(projectID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (h *Handlers) ResumeProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnResume != nil {
		h.OnResume(projectID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *Handlers) CancelProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnCancel != nil {
		h.OnCancel(projectID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *Handlers) GetStatus(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnGetStatus != nil {
		data, err := h.OnGetStatus(projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"project_id": projectID, "status": "unknown"})
}

func (h *Handlers) GetTasks(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnGetTasks != nil {
		data, _ := h.OnGetTasks(projectID)
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"project_id": projectID, "tasks": []interface{}{}})
}

func (h *Handlers) GetAttempts(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	taskID := r.PathValue("tid")
	if h.OnGetAttempts != nil {
		data, _ := h.OnGetAttempts(projectID, taskID)
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"task_id": taskID, "attempts": []interface{}{}})
}

func (h *Handlers) GetCosts(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnGetCosts != nil {
		data, _ := h.OnGetCosts(projectID)
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"project_id": projectID, "total_cost": 0})
}

func (h *Handlers) GetEvents(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if h.OnGetEvents != nil {
		data, _ := h.OnGetEvents(projectID)
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"project_id": projectID, "events": []interface{}{}})
}

func (h *Handlers) GetModels(w http.ResponseWriter, r *http.Request) {
	if h.OnGetModels != nil {
		data, _ := h.OnGetModels()
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": []interface{}{}})
}

func (h *Handlers) QueryIndex(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QueryType string            `json:"query_type"`
		Params    map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if h.OnQueryIndex != nil {
		data, err := h.OnQueryIndex(req.QueryType, req.Params)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"results": []interface{}{}})
}
