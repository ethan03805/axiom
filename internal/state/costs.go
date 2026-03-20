package state

import (
	"database/sql"
	"fmt"
)

// InsertCost persists a cost log entry to the database.
// AttemptID of 0 is treated as NULL (no associated attempt).
func (db *DB) InsertCost(entry *CostEntry) error {
	var attemptID interface{}
	if entry.AttemptID != 0 {
		attemptID = entry.AttemptID
	} else {
		attemptID = sql.NullInt64{}
	}

	db.wmu.Lock()
	result, err := db.conn.Exec(
		`INSERT INTO cost_log (task_id, attempt_id, agent_type, model_id, input_tokens, output_tokens, cost_usd, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		nullString(entry.TaskID), attemptID, entry.AgentType,
		entry.ModelID, entry.InputTokens, entry.OutputTokens,
		entry.CostUSD, entry.Timestamp,
	)
	db.wmu.Unlock()
	if err != nil {
		return fmt.Errorf("insert cost: %w", err)
	}
	id, err := result.LastInsertId()
	if err == nil {
		entry.ID = id
	}
	return nil
}

// GetTaskCost returns the total cost in USD for a given task.
func (db *DB) GetTaskCost(taskID string) (float64, error) {
	var total float64
	err := db.conn.QueryRow(
		"SELECT COALESCE(SUM(cost_usd), 0) FROM cost_log WHERE task_id = ?", taskID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get task cost: %w", err)
	}
	return total, nil
}

// GetProjectCost returns the total cost in USD across all tasks.
func (db *DB) GetProjectCost() (float64, error) {
	var total float64
	err := db.conn.QueryRow(
		"SELECT COALESCE(SUM(cost_usd), 0) FROM cost_log",
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get project cost: %w", err)
	}
	return total, nil
}

// GetCostByModel returns a map of model_id to total cost.
func (db *DB) GetCostByModel() (map[string]float64, error) {
	rows, err := db.conn.Query(
		"SELECT model_id, SUM(cost_usd) FROM cost_log GROUP BY model_id",
	)
	if err != nil {
		return nil, fmt.Errorf("get cost by model: %w", err)
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var model string
		var cost float64
		if err := rows.Scan(&model, &cost); err != nil {
			return nil, fmt.Errorf("scan cost by model: %w", err)
		}
		result[model] = cost
	}
	return result, rows.Err()
}

// GetCostByAgentType returns a map of agent_type to total cost.
func (db *DB) GetCostByAgentType() (map[string]float64, error) {
	rows, err := db.conn.Query(
		"SELECT agent_type, SUM(cost_usd) FROM cost_log GROUP BY agent_type",
	)
	if err != nil {
		return nil, fmt.Errorf("get cost by agent type: %w", err)
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var agentType string
		var cost float64
		if err := rows.Scan(&agentType, &cost); err != nil {
			return nil, fmt.Errorf("scan cost by agent type: %w", err)
		}
		result[agentType] = cost
	}
	return result, rows.Err()
}
